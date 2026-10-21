// Command u5 runs Ultima V: Warriors of Destiny (the DOS EGA version) from the original
// data files, its program (ULTIMA.EXE and the overlays) ported to Go.
//
//	u5 [-data DIR]            play with the game files in DIR (default $GOPLAY_DATA, else ./game)
//	u5 -import SRC -data DIR  copy the game files out of SRC (folder, zip, ISO/BIN/CUE, GOG
//	                          installer, loose files) into DIR, write DIR/COVER.PNG and exit;
//	                          the Exodus Project's Ultima V Upgrade in SRC gives the MIDI music
//	u5 -import-disk2 SRC -data DIR
//	                          copy the Amiga extras (the music) out of the Amiga disks in SRC
//	                          (.adf images, folder, zip) into DIR/DISK2 and exit
//	u5 -save DIR              keep the saved games in DIR rather than the game directory
//	u5 -music midi|amiga|off  the music to start with (default midi when the Exodus upgrade's
//	                          XMI files are there, else amiga when DISK2 is there)
//	u5 -nomusic               start with the music off
//
// F2 opens the settings (music source, volume, sound effects); Alt-V mutes the music,
// Alt-'+' and Alt-'-' set its volume.
package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/memmaker/u5-go/internal/game"
	"github.com/memmaker/u5-go/internal/host"
	"github.com/memmaker/u5-go/internal/music"
	"github.com/memmaker/u5-go/internal/speaker"
	"github.com/retro-ware/gameimport"
)

// markerFiles prove a folder holds Ultima V.
var markerFiles = []string{"DATA.OVL", "T1K.DRV", "TILES.16", "BRIT.DAT"}

// Permissions of the directories and files written.
const (
	dirMode  = 0o755
	fileMode = 0o644
)

// gameFile says whether name is one of the game's data files: everything on the original
// disks but the programs, and none of what GOG packs around them (DOSBox, its
// redistributables) but the starting saves GOG keeps in __support/save.
func gameFile(name string) bool {
	parts := strings.Split(strings.ToUpper(name), "/")
	for i, part := range parts[:len(parts)-1] {
		switch part {
		case "DOSBOX", "__REDIST", "APP", "CLOUD_SAVES":
			return false
		case "__SUPPORT":
			if i+1 >= len(parts)-1 || parts[i+1] != "SAVE" {
				return false
			}
		}
	}
	base := parts[len(parts)-1]
	switch path.Ext(base) {
	case ".EXE", ".BAT", ".BIN", ".IMG", ".IMA", ".ISO", ".ZIP", ".CUE", ".GOG", ".7Z", ".COM",
		".DLL", ".INI", ".TXT", ".PDF", ".DOC", ".CONF", ".INFO", ".SCRIPT", ".HASHDB", ".ICO", ".PNG",
		".JPG", ".TTF", ".JSON", "":
		return false
	}
	return !strings.ContainsAny(base, " ")
}

func main() {
	defaultDir := os.Getenv("GOPLAY_DATA")
	if defaultDir == "" {
		defaultDir = "game"
		if _, err := os.Stat("data"); err == nil {
			defaultDir = "data" // goplay: started in games/u5 with no arguments
		}
	}
	dir := flag.String("data", defaultDir, "directory with the original game files")
	source := flag.String("import", "", "import the game files from this folder/zip/ISO/installer into -data and exit")
	disk2 := flag.String("import-disk2", "", "import the Amiga extras from these Amiga disks (.adf, folder, zip) into -data/DISK2 and exit")
	save := flag.String("save", "", "directory for saved games (default: the game directory)")
	scale := flag.Int("scale", 3, "window scale")
	musicFlag := flag.String("music", "midi", "music to start with: midi, amiga or off")
	noMusic := flag.Bool("nomusic", false, "start with the music off")
	flag.Parse()

	if *disk2 != "" {
		if err := importDisk2(*disk2, *dir); err != nil {
			fmt.Fprintln(os.Stderr, "import-disk2:", err)
			os.Exit(1)
		}
		return
	}

	if *source != "" {
		if err := importGame(*source, *dir); err != nil {
			fmt.Fprintln(os.Stderr, "import:", err)
			os.Exit(1)
		}
		return
	}
	if _, err := os.Stat(filepath.Join(*dir, markerFiles[0])); err != nil {
		fmt.Fprintf(os.Stderr, "no game files in %s; use -import SRC or -data DIR\n", *dir)
		os.Exit(1)
	}
	if *save != "" {
		if err := os.MkdirAll(*save, dirMode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		game.SaveDir = *save
	}
	if err := game.Install(*dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	preferred := map[string]music.Source{"midi": music.MIDI, "amiga": music.Amiga, "off": music.Off}[strings.ToLower(*musicFlag)]
	if *noMusic {
		preferred = music.Off
	}
	if b, err := os.ReadFile(filepath.Join(*dir, harpFile)); err == nil && speaker.LoadHarp(b) == nil {
		speaker.HarpEnabled = true
	}
	speaker.Start()
	music.Start(*dir, preferred)
	setUpSettings()
	if err := host.Run("Ultima V", *scale, game.Run); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// importGame copies the game files found in source into dir and writes the cover picture.
// Files at the top of a folder win over copies deeper down (GOG's saves). The Exodus
// Project's Ultima V Upgrade, when it is in the drop, gives its music (the XMI files); the
// patched programs it ships beside them never replace the originals. A drop of the upgrade
// alone adds its music to the game already in dir.
func importGame(source, dir string) error {
	files, err := gameimport.Find(source, gameFile)
	if err != nil {
		return err
	}
	upgradeDirs := map[string]bool{}
	for name := range files {
		if isMusicFile(name) {
			upgradeDirs[path.Dir(name)] = true
		}
	}
	out := map[string][]byte{}
	rank := map[string]int{} // lower wins: the depth, the upgrade's files after all others
	const upgradeRank = 1 << 16
	for name, contents := range files {
		base := strings.ToUpper(path.Base(name))
		r := strings.Count(name, "/")
		if upgradeDirs[path.Dir(name)] && !isMusicFile(name) {
			r += upgradeRank
		}
		if old, ok := rank[base]; ok && old <= r {
			continue
		}
		out[base], rank[base] = contents, r
	}
	for _, marker := range markerFiles {
		if out[marker] != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil && len(upgradeDirs) > 0 {
			out = musicOnly(out) // the upgrade for a game already imported
			break
		}
		return fmt.Errorf("no Ultima V files (%s) found in %s", marker, source)
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	for name, contents := range out {
		if err := os.WriteFile(filepath.Join(dir, name), contents, fileMode); err != nil {
			return err
		}
	}
	return writeCover(dir)
}

// isMusicFile says whether name is one of the upgrade's songs.
func isMusicFile(name string) bool {
	base := strings.ToUpper(path.Base(name))
	for _, song := range music.SongFiles {
		if base == song {
			return true
		}
	}
	return false
}

func musicOnly(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for name, contents := range files {
		if isMusicFile(name) {
			out[name] = contents
		}
	}
	return out
}
