package utils

import (
	"crypto/rand"
	"crypto/sha1"
)

func MakePeerID() string {
	peerIDB := make([]byte, 20)
	rand.Read(peerIDB)

	var peerID [20]byte
	copy(peerID[:], peerIDB)

	return BinaryToJsonString(peerID[:])
}

func MakeInfoHash(s string) string {
	var infoHash [20]byte
	hash := sha1.New()
	hash.Write([]byte(s))
	copy(infoHash[:], hash.Sum(nil))

	return BinaryToJsonString(infoHash[:])
}

func BinaryToJsonString(b []byte) string {
	var seq []rune
	for _, v := range b {
		seq = append(seq, rune(v))
	}
	return string(seq)
}
