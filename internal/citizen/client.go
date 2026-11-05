package citizen

import (
	"bytes"
	"civora/internal/archive"
	"civora/internal/chain"
	"civora/internal/codec"
	"civora/internal/consensus"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func Client() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect refused") }}
}
func GetBounded(client *http.Client, url string, max int) ([]byte, error) {
	req, e := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if e != nil {
		return nil, e
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("source HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, int64(max)+1))
	if e != nil || len(b) > max {
		return nil, fmt.Errorf("source capacity")
	}
	return b, nil
}

type HTTPProvider struct {
	Genesis    *chain.Genesis
	Descriptor Chain
	Client     *http.Client
	turn       atomic.Uint64
}

func (p *HTTPProvider) each(path string, max int, accept func([]byte) error) ([]byte, error) {
	start := int(p.turn.Add(1) - 1)
	var last error
	for i := range p.Descriptor.Origins {
		o := p.Descriptor.Origins[(start+i)%len(p.Descriptor.Origins)]
		raw, e := GetBounded(p.Client, strings.TrimRight(o.API, "/")+"/v1/chains/"+p.Descriptor.ChainID+path, max)
		if e == nil && accept != nil {
			e = accept(raw)
		}
		if e == nil {
			return raw, nil
		}
		last = e
	}
	return nil, fmt.Errorf("all sources unavailable: %w", last)
}
func (p *HTTPProvider) Height() uint64 {
	var height uint64
	_, _ = p.each("/blocks", 4096, func(raw []byte) error {
		var v struct {
			Chain  string `json:"chain_id"`
			Height string `json:"height"`
		}
		if _, e := codec.Parse(raw); e != nil {
			return e
		}
		if e := json.Unmarshal(raw, &v); e != nil {
			return e
		}
		h, e := codec.Uint(v.Height)
		if e != nil || v.Chain != p.Descriptor.ChainID {
			return fmt.Errorf("source network")
		}
		height = h
		return nil
	})
	return height
}
func (p *HTTPProvider) Bundle(h uint64) (chain.Bundle, error) {
	var b chain.Bundle
	_, e := p.each("/blocks/"+fmt.Sprint(h), 8*1024*1024, func(raw []byte) error {
		if e := codec.Decode(raw, "archive_block", &b); e != nil {
			return e
		}
		header, e := consensus.VerifyHeader(p.Genesis.TrustAt(h), b.SignedHeader)
		if e != nil || header.Height != int64(h) {
			return fmt.Errorf("untrusted block header")
		}
		block, e := chain.Decode(b)
		if e != nil || !bytes.Equal(block.Hash(), header.Hash()) {
			return fmt.Errorf("source block integrity")
		}
		return nil
	})
	return b, e
}
func FetchGenesis(c Chain) ([]byte, error) {
	client := Client()
	var last error
	for _, o := range c.Origins {
		raw, e := GetBounded(client, strings.TrimRight(o.API, "/")+"/v1/chains/"+c.ChainID+"/genesis", 8*1024*1024)
		if e == nil && (archive.Digest(raw) != c.Genesis.SHA256 || fmt.Sprint(len(raw)) != c.Genesis.Size) {
			e = fmt.Errorf("genesis digest mismatch")
		}
		if e == nil {
			return raw, nil
		}
		last = e
	}
	return nil, last
}
func Broadcast(c Chain, raw []byte) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "broadcast_tx_sync", "params": map[string]any{"tx": base64.StdEncoding.EncodeToString(raw)}})
	for _, o := range c.Origins {
		req, e := http.NewRequest("POST", o.RPC, bytes.NewReader(body))
		if e != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		res, e := Client().Do(req)
		if e != nil {
			continue
		}
		b, e := io.ReadAll(io.LimitReader(res.Body, 65537))
		_ = res.Body.Close()
		if e != nil || len(b) > 65536 {
			continue
		}
		var reply struct {
			Error  json.RawMessage `json:"error"`
			Result struct {
				Code uint32 `json:"code"`
			} `json:"result"`
		}
		if json.Unmarshal(b, &reply) != nil {
			continue
		}
		if len(reply.Error) > 0 && string(reply.Error) != "null" {
			continue
		}
		if reply.Result.Code != 0 {
			return fmt.Errorf("transaction refused by source")
		}
		return nil
	}
	return fmt.Errorf("no relay accepted transaction")
}

// FetchObject resumes a previously saved prefix, but accepts nothing until the
// complete object has the exact content hash and size advertised by the record.
func FetchObject(c Chain, store archive.Store, ref archive.Ref) error {
	objectDownloads.Lock()
	defer objectDownloads.Unlock()
	if _, e := store.Get(ref); e == nil {
		return nil
	}
	size, e := codec.Uint(ref.Size)
	if e != nil || size > archive.MaxObjectBytes {
		return fmt.Errorf("object bound")
	}
	path, e := store.Path(ref.SHA256)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	partial := path + ".partial"
	var last error
	for _, o := range c.Origins {
		for attempt := 0; attempt < 2; attempt++ {
			prefix, _ := os.ReadFile(partial)
			if uint64(len(prefix)) > size {
				prefix = nil
			}
			if uint64(len(prefix)) == size && archive.Digest(prefix) != ref.SHA256 {
				prefix = nil
				_ = os.WriteFile(partial, nil, 0600)
			}
			if uint64(len(prefix)) == size && archive.Digest(prefix) == ref.SHA256 {
				_, e = store.Put(prefix)
				return e
			}
			req, err := http.NewRequest("GET", strings.TrimRight(o.API, "/")+"/v1/objects/"+ref.SHA256, nil)
			if err != nil {
				return err
			}
			if len(prefix) > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(prefix)))
				req.Header.Set("If-Range", "\""+ref.SHA256+"\"")
			}
			res, err := Client().Do(req)
			if err != nil {
				last = err
				break
			}
			if res.StatusCode == 200 {
				prefix = nil
			} else if res.StatusCode != 206 || len(prefix) == 0 || res.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", len(prefix), size-1, size) {
				res.Body.Close()
				last = fmt.Errorf("invalid range response")
				break
			}
			body, readErr := io.ReadAll(io.LimitReader(res.Body, int64(size)-int64(len(prefix))+1))
			res.Body.Close()
			raw := append(prefix, body...)
			if uint64(len(raw)) > size {
				last = fmt.Errorf("object bound")
				break
			}
			if err = os.WriteFile(partial, raw, 0600); err != nil {
				return err
			}
			if readErr != nil || uint64(len(raw)) != size {
				last = fmt.Errorf("incomplete object: %v", readErr)
				break
			}
			if archive.Digest(raw) != ref.SHA256 {
				_ = os.WriteFile(partial, nil, 0600)
				last = fmt.Errorf("mirror object corruption")
				continue
			}
			_, err = store.Put(raw)
			if err == nil {
				_ = os.Remove(partial)
				return nil
			}
			last = err
			break
		}
	}
	return last
}

var objectDownloads sync.Mutex
