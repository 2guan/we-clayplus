package ability_test

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	favorapi "github.com/we-clay/we-clay/gateway/internal/api/favor"
	"github.com/we-clay/we-clay/sdk/favor"
)

func TestFavoriteItemAndBatchRoutes(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var malformed atomic.Bool
	var missingCode atomic.Bool
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/api/favor/item/42":
			if r.Method == http.MethodDelete {
				id := 42
				if malformed.Load() {
					id = 43
				}
				code := `,"code":0`
				if missingCode.Load() {
					code = ""
				}
				io.WriteString(w, `{"code":0,"data":{"count":1,"results":[{"id":`+strconv.Itoa(id)+code+`}]}}`)
				return
			}
			id := 42
			if malformed.Load() {
				id = 43
			}
			io.WriteString(w, `{"code":0,"data":{"count":1,"objects":[{"id":`+strconv.Itoa(id)+`,"object":"<fav/>"}]}}`)
		case "/api/favor/items/batch-get", "/api/favor/items/batch-delete":
			if r.Method != http.MethodPost {
				t.Errorf("batch route method = %s", r.Method)
			}
			var q struct {
				IDs []int32 `json:"ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil || !reflect.DeepEqual(q.IDs, []int32{42, 43}) {
				t.Errorf("batch request IDs = %v, error = %v", q.IDs, err)
			}
			if r.URL.Path == "/api/favor/items/batch-get" {
				io.WriteString(w, `{"code":0,"data":{"count":2,"objects":[{"id":42},{"id":43}]}}`)
			} else {
				io.WriteString(w, `{"code":0,"data":{"count":2,"results":[{"id":42,"code":0},{"id":43,"code":0}]}}`)
			}
		default:
			t.Errorf("unexpected favorite path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	items, err := favor.Instance.GetItem(42)
	if err != nil || len(items) != 1 || items[0].GetId() != 42 || items[0].GetObject() != "<fav/>" {
		t.Fatalf("single item: %v, %v", items, err)
	}
	if err := favor.Instance.Delete(42); err != nil {
		t.Fatal(err)
	}
	batch, err := favorapi.Get().BatchGetItems([]int32{42, 43})
	if err != nil || batch.GetCount() != 2 || len(batch.GetObjects()) != 2 {
		t.Fatalf("batch get: %v, %v", batch, err)
	}
	deleted, err := favorapi.Get().BatchDelete([]int32{42, 43})
	if err != nil || deleted.GetCount() != 2 || len(deleted.GetResults()) != 2 {
		t.Fatalf("batch delete: %v, %v", deleted, err)
	}
	mu.Lock()
	gotPaths := append([]string(nil), paths...)
	mu.Unlock()
	if !reflect.DeepEqual(gotPaths, []string{
		"GET /api/favor/item/42", "DELETE /api/favor/item/42",
		"POST /api/favor/items/batch-get", "POST /api/favor/items/batch-delete",
	}) {
		t.Fatalf("favorite route calls: %v", gotPaths)
	}
	malformed.Store(true)
	if _, err := favor.Instance.GetItem(42); err == nil {
		t.Fatal("mismatched item ID accepted")
	}
	if err := favor.Instance.Delete(42); err == nil {
		t.Fatal("mismatched deletion receipt accepted")
	}
	malformed.Store(false)
	missingCode.Store(true)
	if err := favor.Instance.Delete(42); err == nil {
		t.Fatal("deletion receipt without item status accepted")
	}
}
