/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package history provides an append only, size limited log of recent actions
// that Keeper has taken for each subpool.
package history

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/jenkins-x/lighthouse/pkg/apis/lighthouse/v1alpha1"
	"github.com/sirupsen/logrus"
)

// Mock out time for unit testing.
var now = time.Now

// History uses a `*recordLog` per pool to store a record of recent actions that
// Keeper has taken. Using a log per pool ensure that history is retained
// for inactive pools even if other pools are very active.
type History struct {
	logs map[string]*recordLog
	sync.Mutex
	logSizeLimit int

	path string
}

func readHistory(maxRecordsPerKey int, path string) (map[string]*recordLog, error) {
	// TODO: This all needs to be removed eventually but for the moment, we're just going to make history no-op. (apb)
	return map[string]*recordLog{}, nil
}

func writeHistory(path string, hist map[string][]*Record) error {
	// TODO: This all needs to be removed eventually but for the moment, we're just going to make history no-op. (apb)
	return nil
}

// Record is an entry describing one action that Keeper has taken (e.g. TRIGGER or MERGE).
type Record struct {
	Time    time.Time       `json:"time"`
	Action  string          `json:"action"`
	BaseSHA string          `json:"baseSHA,omitempty"`
	Target  []v1alpha1.Pull `json:"target,omitempty"`
	Err     string          `json:"err,omitempty"`
}

// New creates a new History struct with the specificed recordLog size limit.
func New(maxRecordsPerKey int, path string) (*History, error) {
	hist := &History{
		logs:         map[string]*recordLog{},
		logSizeLimit: maxRecordsPerKey,
		path:         path,
	}

	if path != "" {
		// Load existing history from GCS.
		var err error
		start := time.Now()
		hist.logs, err = readHistory(maxRecordsPerKey, hist.path)
		if err != nil {
			return nil, err
		}
		logrus.WithFields(logrus.Fields{
			"duration": time.Since(start).String(),
			"path":     hist.path,
		}).Debugf("Successfully read action history for %d pools.", len(hist.logs))
	}

	return hist, nil
}

// Record appends an entry to the recordlog specified by the poolKey.
func (h *History) Record(poolKey, action, baseSHA, err string, targets []v1alpha1.Pull) {
	t := now()
	sort.Sort(v1alpha1.ByNum(targets))
	h.addRecord(
		poolKey,
		&Record{
			Time:    t,
			Action:  action,
			BaseSHA: baseSHA,
			Target:  targets,
			Err:     err,
		},
	)
}

func (h *History) addRecord(poolKey string, rec *Record) {
	h.Lock()
	defer h.Unlock()
	if _, ok := h.logs[poolKey]; !ok {
		h.logs[poolKey] = newRecordLog(h.logSizeLimit)
	}
	h.logs[poolKey].add(rec)
}

// ServeHTTP serves a JSON mapping from pool key -> sorted records for the pool.
func (h *History) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, err := json.Marshal(h.AllRecords())
	if err != nil {
		logrus.WithError(err).Error("Encoding JSON history.")
		b = []byte("{}")
	}
	if _, err = w.Write(b); err != nil {
		logrus.WithError(err).Error("Writing JSON history response.")
	}
}

// Flush writes the action history to persistent storage if configured to do so.
func (h *History) Flush() {
	if h.path == "" {
		return
	}
	records := h.AllRecords()
	start := time.Now()
	err := writeHistory(h.path, records)
	log := logrus.WithFields(logrus.Fields{
		"duration": time.Since(start).String(),
		"path":     h.path,
	})
	if err != nil {
		log.WithError(err).Error("Error flushing action history to GCS.")
	} else {
		log.Debugf("Successfully flushed action history for %d pools.", len(h.logs))
	}
}

// AllRecords generates a map from pool key -> sorted records for the pool.
func (h *History) AllRecords() map[string][]*Record {
	h.Lock()
	defer h.Unlock()

	res := make(map[string][]*Record, len(h.logs))
	for key, log := range h.logs {
		res[key] = log.toSlice()
	}
	return res
}

// Merge combines the logs from the other history
func (h *History) Merge(other *History) {
	otherLogs := other.AllRecords()

	for key, logs := range otherLogs {
		for _, log := range logs {
			h.addRecord(key, log)
		}
	}
}

// recordLog is a space efficient, limited size, append only list.
type recordLog struct {
	buff  []*Record
	head  int
	limit int

	// cachedSlice is the cached, in-order slice. Use toSlice(), don't access directly.
	// We cache this value because most pools don't change between sync loops.
	cachedSlice []*Record
}

func newRecordLog(sizeLimit int) *recordLog {
	return &recordLog{
		head:  -1,
		limit: sizeLimit,
	}
}

func (rl *recordLog) add(rec *Record) {
	// Start by invalidating cached slice.
	rl.cachedSlice = nil

	rl.head = (rl.head + 1) % rl.limit
	if len(rl.buff) < rl.limit {
		// The log is not yet full. Append the record.
		rl.buff = append(rl.buff, rec)
	} else {
		// The log is full. Overwrite the oldest record.
		rl.buff[rl.head] = rec
	}
}

func (rl *recordLog) toSlice() []*Record {
	if rl.cachedSlice != nil {
		return rl.cachedSlice
	}

	res := make([]*Record, 0, len(rl.buff))
	for i := 0; i < len(rl.buff); i++ {
		index := (rl.limit + rl.head - i) % rl.limit
		res = append(res, rl.buff[index])
	}
	rl.cachedSlice = res
	return res
}
