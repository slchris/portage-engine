package iac

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeCluster serves a /api2/json/cluster/resources fixture and records the
// Authorization header it saw.
func fakeCluster(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, clusterFixture)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotAuth
}

const clusterFixture = `{"data":[
	{"type":"node","node":"pve1","status":"online","maxmem":68719476736,"mem":60129542144,"cpu":0.10},
	{"type":"node","node":"pve2","status":"online","maxmem":68719476736,"mem":17179869184,"cpu":0.50},
	{"type":"node","node":"pve3","status":"offline","maxmem":137438953472,"mem":0,"cpu":0},
	{"type":"qemu","node":"pve1","name":"debian-12-cloudinit-template","template":1,"status":"stopped"},
	{"type":"qemu","node":"pve2","name":"debian-12-cloudinit-template","template":1,"status":"stopped"},
	{"type":"qemu","node":"pve2","name":"some-running-vm","template":0,"status":"running"}
]}`

func TestSelectPVENode_PicksLeastLoadedTemplateNode(t *testing.T) {
	srv, gotAuth := fakeCluster(t)

	// No candidate list: restrict to nodes hosting the template (pve1, pve2).
	// pve2 has far more free memory and must win; offline pve3 is ignored.
	node, err := SelectPVENode(srv.URL, PVEAuth{TokenID: "root@pam!terraform", TokenSecret: "s3cr3t"}, nil, "debian-12-cloudinit-template")
	if err != nil {
		t.Fatalf("SelectPVENode: %v", err)
	}
	if node != "pve2" {
		t.Errorf("expected pve2 (most free memory), got %q", node)
	}
	if want := "PVEAPIToken=root@pam!terraform=s3cr3t"; *gotAuth != want {
		t.Errorf("auth header = %q, want %q", *gotAuth, want)
	}
}

func TestSelectPVENode_CandidateListWins(t *testing.T) {
	srv, _ := fakeCluster(t)

	// Explicit candidates override the template restriction: only pve1 allowed.
	node, err := SelectPVENode(srv.URL, PVEAuth{TokenID: "root@pam!t", TokenSecret: "s"}, []string{"pve1"}, "debian-12-cloudinit-template")
	if err != nil {
		t.Fatalf("SelectPVENode: %v", err)
	}
	if node != "pve1" {
		t.Errorf("expected pve1 (only candidate), got %q", node)
	}
}

func TestSelectPVENode_TemplateMissing(t *testing.T) {
	srv, _ := fakeCluster(t)

	if _, err := SelectPVENode(srv.URL, PVEAuth{TokenID: "root@pam!t", TokenSecret: "s"}, nil, "no-such-template"); err == nil {
		t.Fatal("expected an error when no node hosts the template")
	}
}

func TestSelectPVENode_AllEligibleOffline(t *testing.T) {
	srv, _ := fakeCluster(t)

	if _, err := SelectPVENode(srv.URL, PVEAuth{TokenID: "root@pam!t", TokenSecret: "s"}, []string{"pve3"}, ""); err == nil {
		t.Fatal("expected an error when every eligible node is offline")
	}
}

func TestSelectPVENode_RequiresTokenAuth(t *testing.T) {
	if _, err := SelectPVENode("https://pve:8006", PVEAuth{}, nil, "tpl"); err == nil {
		t.Fatal("expected an error without token credentials")
	}
}

func TestNodeLessLoaded(t *testing.T) {
	moreFree := &pveClusterResource{MaxMem: 100, Mem: 10, CPU: 0.9}
	lessFree := &pveClusterResource{MaxMem: 100, Mem: 50, CPU: 0.1}
	if !nodeLessLoaded(moreFree, lessFree) {
		t.Error("node with more free memory should win regardless of CPU")
	}

	tiedA := &pveClusterResource{MaxMem: 100, Mem: 50, CPU: 0.2}
	tiedB := &pveClusterResource{MaxMem: 100, Mem: 50, CPU: 0.8}
	if !nodeLessLoaded(tiedA, tiedB) {
		t.Error("on equal free memory, lower CPU load should win")
	}
}
