package momentsability

import (
	"fmt"
	"net/http"
	"testing"
)

func TestMomentSyncHasJSONContentTypeAndPreservesOpaqueCommands(t *testing.T) {
	clients := commentClients(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/moments/sync" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("SNS同步缺少core要求的JSON类型")
		}
		fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0},"commands":{"count":1,"list":[{"id":46,"buffer":{"size":2,"data":"CAI="}}]},"key":{"size":2,"data":"AQI="}}}`)
	})
	for _, client := range clients {
		r, err := client.Sync([]byte{1, 2})
		if err != nil || len(r.GetCommands()) != 1 || r.GetCommands()[0].GetId() != 46 || len(r.GetCommands()[0].GetBuffer().GetData()) != 2 {
			t.Fatal("SNS命令被丢弃", r, err)
		}
	}
}
