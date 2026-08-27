package idgen

import (
	"crypto/rand"

	"github.com/oklog/ulid/v2"
)

func New() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}
