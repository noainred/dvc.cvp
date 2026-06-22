// Package history persists downsampled throughput and port-usage series to an
// embedded bbolt database so trends survive restarts and extend well beyond the
// in-memory ring. Samples are written at most once per minute; data older than
// the configured retention is pruned. The long-term series feed the capacity
// trend / projection views.
package history

import (
	"encoding/binary"
	"encoding/json"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketSummary = []byte("summary") // key: unix-minute big-endian -> SummaryPoint
	bucketDevice  = []byte("device")  // key: serial \x00 unix-minute -> Sample(in/out)
)

// SummaryPoint is one minute of global capacity/throughput history.
type SummaryPoint struct {
	T         time.Time `json:"t"`
	PortUsed  int       `json:"portUsed"`
	PortTotal int       `json:"portTotal"`
	InBps     float64   `json:"inBps"`
	OutBps    float64   `json:"outBps"`
}

// Store is the bbolt-backed history store.
type Store struct {
	db        *bolt.DB
	retention time.Duration
	lastMin   int64
	lastPrune time.Time
}

// Open creates/opens the history database at path.
func Open(path string, retention time.Duration) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketSummary, bucketDevice} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, retention: retention}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Record persists a snapshot, throttled to one sample per wall-clock minute.
func (s *Store) Record(sum model.Summary, devices []model.Device) {
	if s == nil {
		return
	}
	now := time.Now()
	minute := now.Unix() / 60
	if minute == s.lastMin {
		return // already recorded this minute
	}
	s.lastMin = minute

	_ = s.db.Update(func(tx *bolt.Tx) error {
		mkey := u64(uint64(minute * 60))
		sp, _ := json.Marshal(SummaryPoint{
			T: now, PortUsed: sum.PortUsed, PortTotal: sum.PortTotal,
			InBps: sum.TotalInBps, OutBps: sum.TotalOutBps,
		})
		_ = tx.Bucket(bucketSummary).Put(mkey, sp)

		db := tx.Bucket(bucketDevice)
		for _, d := range devices {
			v, _ := json.Marshal(model.Sample{T: now, InBps: d.InBps, OutBps: d.OutBps})
			_ = db.Put(devKey(d.Serial, minute*60), v)
		}
		return nil
	})

	if now.Sub(s.lastPrune) > time.Hour {
		s.lastPrune = now
		s.prune(now.Add(-s.retention))
	}
}

// SummarySeries returns global history points since t (chronological order).
func (s *Store) SummarySeries(since time.Time) []SummaryPoint {
	out := []SummaryPoint{}
	if s == nil {
		return out
	}
	lo := u64(uint64(since.Unix()))
	_ = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketSummary).Cursor()
		for k, v := c.Seek(lo); k != nil; k, v = c.Next() {
			var p SummaryPoint
			if json.Unmarshal(v, &p) == nil {
				out = append(out, p)
			}
		}
		return nil
	})
	return out
}

// DeviceSeries returns a device's throughput history since t.
func (s *Store) DeviceSeries(serial string, since time.Time) []model.Sample {
	out := []model.Sample{}
	if s == nil {
		return out
	}
	prefix := []byte(serial + "\x00")
	lo := devKey(serial, since.Unix())
	_ = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketDevice).Cursor()
		for k, v := c.Seek(lo); k != nil && hasPrefix(k, prefix); k, v = c.Next() {
			var smp model.Sample
			if json.Unmarshal(v, &smp) == nil {
				out = append(out, smp)
			}
		}
		return nil
	})
	return out
}

// prune deletes summary and device samples older than cutoff.
func (s *Store) prune(cutoff time.Time) {
	hi := u64(uint64(cutoff.Unix()))
	_ = s.db.Update(func(tx *bolt.Tx) error {
		sc := tx.Bucket(bucketSummary).Cursor()
		for k, _ := sc.First(); k != nil && bytesLess(k, hi); k, _ = sc.First() {
			if err := sc.Delete(); err != nil {
				break
			}
		}
		// device keys are serial-prefixed; scan all and delete by trailing time.
		dc := tx.Bucket(bucketDevice).Cursor()
		for k, _ := dc.First(); k != nil; k, _ = dc.Next() {
			if len(k) >= 8 && bytesLess(k[len(k)-8:], hi) {
				_ = dc.Delete()
			}
		}
		return nil
	})
}

func devKey(serial string, unixSec int64) []byte {
	b := append([]byte(serial), 0)
	return append(b, u64(uint64(unixSec))...)
}

func u64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func hasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	for i := range prefix {
		if b[i] != prefix[i] {
			return false
		}
	}
	return true
}

// bytesLess reports whether the 8-byte big-endian time in b is < hi.
func bytesLess(b, hi []byte) bool {
	if len(b) < 8 || len(hi) < 8 {
		return false
	}
	return binary.BigEndian.Uint64(b[len(b)-8:]) < binary.BigEndian.Uint64(hi)
}
