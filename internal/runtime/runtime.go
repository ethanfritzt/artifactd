package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"artifactd/internal/protocol"
)

var ErrNotFound = errors.New("runtime data not found")

var sourcePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Store contains the latest live data for each artifact source. Runtime data is
// intentionally ephemeral; immutable artifact versions remain in storage.
type Store struct {
	mu   sync.RWMutex
	data map[string]protocol.DataResponse
}

func NewStore() *Store {
	return &Store{data: make(map[string]protocol.DataResponse)}
}

func (s *Store) Put(artifactID, source string, data []byte) (protocol.DataResponse, error) {
	if !sourcePattern.MatchString(source) || len(source) > 63 {
		return protocol.DataResponse{}, fmt.Errorf("invalid data source")
	}
	if !json.Valid(data) {
		return protocol.DataResponse{}, fmt.Errorf("data must be valid JSON")
	}
	value := append(json.RawMessage(nil), data...)
	response := protocol.DataResponse{
		ArtifactID: artifactID,
		Source:     source,
		UpdatedAt:  time.Now().UTC(),
		Data:       value,
	}
	s.mu.Lock()
	s.data[key(artifactID, source)] = response
	s.mu.Unlock()
	return response, nil
}

func (s *Store) Get(artifactID, source string) (protocol.DataResponse, error) {
	s.mu.RLock()
	response, ok := s.data[key(artifactID, source)]
	s.mu.RUnlock()
	if !ok {
		return protocol.DataResponse{}, ErrNotFound
	}
	response.Data = append(json.RawMessage(nil), response.Data...)
	return response, nil
}

func key(artifactID, source string) string {
	return artifactID + "\x00" + source
}
