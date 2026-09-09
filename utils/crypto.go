package utils

import (
	"crypto/md5"
	"hash"
)

func NewMD5Hash() hash.Hash {
	return md5.New()
}
