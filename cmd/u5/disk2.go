package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/memmaker/u5-go/internal/adf"
	"github.com/memmaker/u5-go/internal/music"
	"github.com/retro-ware/gameimport"
)

// The Amiga version's extras worth having: the one thing it does better than the PC is
// sound. Its pictures and tiles are the PC's own, so only the music module and the harp
// sample are kept.
const (
	amigaModuleName = "u5music"
	amigaHarpName   = "harp"
	// harpFile is where the harp sample goes, beside the module.
	harpFile = "DISK2/HARP.8SVX"
)

// moduleTag and harpTag prove the files are what they should be.
var (
	moduleTag = []byte("M.K.")
	harpTag   = []byte("8SVX")
)

const (
	moduleTagAt = 1080
	harpTagAt   = 8
)

// wantDisk2 accepts Amiga disk images and the extras already taken off them.
func wantDisk2(name string) bool {
	base := strings.ToLower(path.Base(name))
	return strings.HasSuffix(base, ".adf") || base == amigaModuleName || base == amigaHarpName ||
		base == strings.ToLower(path.Base(music.AmigaModule)) || base == strings.ToLower(path.Base(harpFile))
}

// importDisk2 takes the Amiga extras out of the disks (or files) in source into dir/DISK2.
func importDisk2(source, dir string) error {
	found, err := gameimport.Find(source, wantDisk2)
	if err != nil {
		return err
	}
	var module, harp []byte
	take := func(b []byte) {
		switch {
		case hasTag(b, moduleTagAt, moduleTag) && module == nil:
			module = b
		case hasTag(b, harpTagAt, harpTag) && harp == nil:
			harp = b
		}
	}
	for name, b := range found {
		if !strings.EqualFold(path.Ext(name), ".adf") {
			take(b)
			continue
		}
		files, err := adf.Files(b)
		if err != nil {
			continue // another game's disk, or AmigaDOS
		}
		take(files[amigaModuleName])
		take(files[amigaHarpName])
	}
	if module == nil {
		return fmt.Errorf("no Ultima V Amiga disk (with the music) found in %s", source)
	}
	if err := os.MkdirAll(filepath.Join(dir, path.Dir(music.AmigaModule)), dirMode); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, music.AmigaModule), module, fileMode); err != nil {
		return err
	}
	if harp != nil {
		return os.WriteFile(filepath.Join(dir, harpFile), harp, fileMode)
	}
	return nil
}

func hasTag(b []byte, at int, tag []byte) bool {
	return len(b) >= at+len(tag) && bytes.Equal(b[at:at+len(tag)], tag)
}
