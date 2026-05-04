package specindex

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

//go:embed index.gen.json.gz
var embeddedIndexJSONGZ []byte

var (
	embeddedOnce sync.Once
	embedded     *Index
	embeddedErr  error
)

func LoadEmbedded() (*Index, error) {
	embeddedOnce.Do(func() {
		reader, err := gzip.NewReader(bytes.NewReader(embeddedIndexJSONGZ))
		if err != nil {
			embeddedErr = fmt.Errorf("open embedded spec index gzip: %w", err)
			return
		}
		defer reader.Close()
		payload, err := io.ReadAll(reader)
		if err != nil {
			embeddedErr = fmt.Errorf("read embedded spec index gzip: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(payload, &idx); err != nil {
			embeddedErr = fmt.Errorf("decode embedded spec index: %w", err)
			return
		}
		embedded = &idx
	})
	if embeddedErr != nil {
		return nil, embeddedErr
	}
	return embedded, nil
}
