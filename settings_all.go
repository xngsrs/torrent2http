// +build !arm

package main

import "github.com/ElementumOrg/libtorrent-go"

// Nothing to do on regular devices
func setPlatformSpecificSettings(settings libtorrent.SettingsPack) {
}
