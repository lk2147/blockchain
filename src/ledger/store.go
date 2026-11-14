// Package ledger stores finalized blocks durably in a node's own LevelDB.
package ledger

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"blockchain-lab01/src/consensus"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type Store struct {
	mu     sync.Mutex
	db     *leveldb.DB
	height uint64
	chain  string
}

func blockKey(height uint64) []byte { return binary.BigEndian.AppendUint64([]byte("block/"), height) }

func Open(path, chain string, readOnly bool) (*Store, error) {
	db, err := leveldb.OpenFile(path, &opt.Options{ReadOnly: readOnly, ErrorIfMissing: readOnly})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, chain: chain}
	fail := func(err error) (*Store, error) {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("%w; close: %v", err, closeErr)
		}
		return nil, err
	}
	stored, err := db.Get([]byte("chain"), nil)
	if err == leveldb.ErrNotFound && !readOnly {
		err = db.Put([]byte("chain"), []byte(chain), &opt.WriteOptions{Sync: true})
		stored = []byte(chain)
	}
	if err != nil {
		return fail(err)
	}
	if !bytes.Equal(stored, []byte(chain)) {
		return fail(fmt.Errorf("ledger belongs to a different chain"))
	}
	h, err := db.Get([]byte("height"), nil)
	if err != nil && err != leveldb.ErrNotFound {
		return fail(err)
	}
	if len(h) > 0 {
		if len(h) != 8 {
			return fail(fmt.Errorf("corrupt ledger height"))
		}
		s.height = binary.BigEndian.Uint64(h)
	}
	return s, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}
func (s *Store) Height() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.height }
func (s *Store) Append(f consensus.Finalized) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Block.Header.ChainID != s.chain {
		return fmt.Errorf("cannot append foreign-chain block")
	}
	if f.Block.Header.Height != s.height+1 {
		return fmt.Errorf("ledger append must be sequential: got %d, want %d", f.Block.Header.Height, s.height+1)
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	batch := new(leveldb.Batch)
	batch.Put(blockKey(f.Block.Header.Height), b)
	batch.Put([]byte("height"), binary.BigEndian.AppendUint64(nil, f.Block.Header.Height))
	// Atomic batch + synchronous WAL before consensus announces the decision.
	if err = s.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
		return err
	}
	s.height = f.Block.Header.Height
	return nil
}
func (s *Store) Get(height uint64) (consensus.Finalized, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f consensus.Finalized
	b, err := s.db.Get(blockKey(height), nil)
	if err != nil {
		return f, err
	}
	err = json.Unmarshal(b, &f)
	return f, err
}

// ReadAll is an explicit inspection/export operation, not a retained RAM ledger.
func (s *Store) ReadAll() ([]consensus.Finalized, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []consensus.Finalized
	it := s.db.NewIterator(util.BytesPrefix([]byte("block/")), nil)
	defer it.Release()
	for it.Next() {
		var f consensus.Finalized
		if err := json.Unmarshal(it.Value(), &f); err != nil {
			return nil, err
		}
		all = append(all, f)
	}
	return all, it.Error()
}
