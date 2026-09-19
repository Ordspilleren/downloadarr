package downloader

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/autobrr/go-torrent/metainfo"
)

// TorrentFileInfo is the metadata the queue needs from an uploaded .torrent
// file: the info hash it is tracked by and a display name for the release
// guess. It mirrors what a magnet link provides through ParseInfoHash and
// MagnetDisplayName.
type TorrentFileInfo struct {
	Hash string // lowercase hex SHA-1 of the info dictionary
	Name string // suggested display name, empty when the file has none
}

// ParseTorrentFile extracts the v1 info hash and name from a .torrent file
// using github.com/autobrr/go-torrent, which handles the malformed optional
// fields real-world trackers serve. The info hash is the SHA-1 of the bencoded
// "info" dictionary exactly as it appears in the file.
func ParseTorrentFile(data []byte) (TorrentFileInfo, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return TorrentFileInfo{}, fmt.Errorf("invalid torrent file: %w", err)
	}
	if len(mi.InfoBytes) == 0 {
		return TorrentFileInfo{}, errors.New("invalid torrent file: missing info dictionary")
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return TorrentFileInfo{}, fmt.Errorf("invalid torrent file: %w", err)
	}
	// A v2-only torrent (BEP 52) has no v1 info hash; the rest of the app,
	// like the magnet parsing, is v1-based, so reject it rather than track a
	// hash the client will never report.
	if !info.HasV1() {
		return TorrentFileInfo{}, errors.New("invalid torrent file: v2-only torrents are not supported")
	}
	return TorrentFileInfo{
		Hash: mi.HashInfoBytes().HexString(),
		Name: info.BestName(),
	}, nil
}
