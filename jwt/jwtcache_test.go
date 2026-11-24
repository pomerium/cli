package jwt

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"

	"github.com/pomerium/pomerium/pkg/cryptutil"
)

func TestLocalCache(t *testing.T) {
	root := t.TempDir()

	c := &LocalCache{
		dir: filepath.Join(root, "jwts"),
	}

	t.Run("NotFound", func(t *testing.T) {
		_, err := c.LoadJWT("NOTFOUND")
		assert.Equal(t, ErrNotFound, err)
	})
	t.Run("Invalid", func(t *testing.T) {
		err := c.StoreJWT("INVALID", "INVALID")
		if !assert.NoError(t, err) {
			return
		}
		_, err = c.LoadJWT("INVALID")
		assert.Equal(t, ErrInvalid, err)
	})
	t.Run("Expired", func(t *testing.T) {
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: cryptutil.NewKey()}, nil)
		if !assert.NoError(t, err) {
			return
		}

		object, err := signer.Sign([]byte(`{"exp": ` + fmt.Sprint(time.Now().Add(-time.Second).Unix()) + `}`))
		if !assert.NoError(t, err) {
			return
		}

		rawJWT, err := object.CompactSerialize()
		if !assert.NoError(t, err) {
			return
		}

		err = c.StoreJWT("EXPIRED", rawJWT)
		if !assert.NoError(t, err) {
			return
		}

		_, err = c.LoadJWT("EXPIRED")
		assert.Equal(t, ErrExpired, err)
	})
}
