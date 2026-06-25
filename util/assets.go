package util

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

var (
	assetVersionOnce sync.Once
	assetVersion     string
)

// StaticAssetVersion returns a short hash of built CSS files for cache busting.
func StaticAssetVersion() string {
	assetVersionOnce.Do(func() {
		h := sha256.New()
		for _, path := range []string{"static/css/tailwind.css", "static/css/app.css"} {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			_, _ = io.Copy(h, f)
			_ = f.Close()
		}
		sum := hex.EncodeToString(h.Sum(nil))
		if len(sum) >= 12 {
			assetVersion = sum[:12]
		} else {
			assetVersion = sum
		}
	})
	return assetVersion
}
