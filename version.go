package main

import (
	"fmt"

	"github.com/ElementumOrg/libtorrent-go"
)

var (
	Version string = "1.2.0" // TODO from git tag
)

func UserAgent() string {
	return fmt.Sprintf("torrent2http/%s libtorrent/%s", Version, libtorrent.Version())
}
