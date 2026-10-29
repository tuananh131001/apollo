package navidrome

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// lastFMSongIDPrefix marks the ids of Last.fm songs, which apollo plays itself
// instead of forwarding to Navidrome.
const lastFMSongIDPrefix = "lastfm-"

// coverArtIDPrefix marks the cover art ids of Last.fm songs, Deezer cover ids
// that apollo fetches itself instead of forwarding to Navidrome.
const coverArtIDPrefix = "dz-"

// LastFMSongID returns the Subsonic song id of a Last.fm track. The id carries
// the artist and title, so it can be played without storing anything.
func LastFMSongID(artist, title string) string {
	// A JSON array keeps the two values apart whatever characters they hold.
	b, _ := json.Marshal([]string{artist, title})
	return lastFMSongIDPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// ParseLastFMSongID returns the artist and title in a LastFMSongID id, and
// false when id is not one.
func ParseLastFMSongID(id string) (artist, title string, ok bool) {
	enc, ok := strings.CutPrefix(id, lastFMSongIDPrefix)
	if !ok {
		return "", "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", "", false
	}
	var v []string
	if err := json.Unmarshal(b, &v); err != nil || len(v) != 2 {
		return "", "", false
	}
	return v[0], v[1], true
}

// lastFMSong returns the Subsonic song of the Last.fm track artist - title,
// with the Deezer album cover cover unless it is empty. Last.fm tracks are not
// in Navidrome, so they have no file details; the fields are still sent
// because clients such as Feishin fail on songs without them. The id leaves
// the cover out, so a song keeps its id whether or not its cover was found.
func lastFMSong(artist, title, cover string) song {
	s := song{
		ID:         LastFMSongID(artist, title),
		Title:      title,
		Artist:     artist,
		Type:       "music",
		DiscNumber: 1,
	}
	if cover != "" {
		s.CoverArt = coverArtIDPrefix + cover
	}
	return s
}
