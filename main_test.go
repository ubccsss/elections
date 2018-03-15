package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func setupTest(t *testing.T) (*server, func()) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Open = true
	c.DBPath = filepath.Join(dir, "test.db")
	c.StudentIDs = filepath.Join(dir, "studentids.txt")
	c.PrivateKey = filepath.Join(dir, "id_rsa")

	if err := ioutil.WriteFile(c.StudentIDs, []byte(`
	  12345678
		23456789
	`), 0700); err != nil {
		t.Fatal(err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	keyDer := x509.MarshalPKCS1PrivateKey(key)
	keyBlock := pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDer,
	}
	keyFile, err := os.Create(c.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	defer keyFile.Close()
	if err := pem.Encode(keyFile, &keyBlock); err != nil {
		t.Fatal(err)
	}

	c.Bios = []Biography{
		{
			Name:  "Candidate 1",
			Image: "foo.png",
			Desc:  "Test",
		},
	}
	c.Positions = []Position{
		{
			Name: "Position 1",
			Desc: "Desc",
			Candidates: []string{
				"Candidate 1",
				"Candidate 2",
			},
		},
		{
			Name: "Position 2",
			Desc: "Desc",
			Candidates: []string{
				"Candidate 3",
			},
		},
		{
			Name: "Position 3",
			Desc: "Desc",
			Candidates: []string{
				"Candidate 4",
			},
		},
		{
			Name: "Position 4",
			Desc: "Desc",
			Candidates: []string{
				"Candidate 5",
			},
		},
	}

	os.Setenv("REMOTE_USER", "test")

	server, err := setup()
	if err != nil {
		t.Fatal(err)
	}

	if err := runMigrate(server.db); err != nil {
		t.Fatal(err)
	}

	return server, func() {
		server.Close()
		os.RemoveAll(dir)
	}
}

func TestIndex(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	// Invalid user set
	{
		os.Setenv("REMOTE_USER", "")
		req := httptest.NewRequest("GET", "/", nil)
		resp := httptest.NewRecorder()
		s.mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("expected StatusInternalServerError")
		}
	}

	// Valid user set
	{
		os.Setenv("REMOTE_USER", "test")
		req := httptest.NewRequest("GET", "/", nil)
		resp := httptest.NewRecorder()
		s.mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected StatusOK; got %s", resp.Body.Bytes())
		}
	}

	// Closed
	{
		c.Open = false
		req := httptest.NewRequest("GET", "/", nil)
		resp := httptest.NewRecorder()
		s.mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("expected StatusInternalServerError; got %s", resp.Body.Bytes())
		}
	}
}

func TestVote(t *testing.T) {
	s, cleanup := setupTest(t)
	defer cleanup()

	{
		req := httptest.NewRequest("POST", "/vote", nil)
		resp := httptest.NewRecorder()
		s.mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("expected StatusInternalServerError")
		}
	}

	{
		req := httptest.NewRequest("POST", "/vote", nil)
		req.Form = url.Values{}
		req.Form.Add("name", "Voter")
		req.Form.Add("student_number", "12345678")
		req.Form.Add(slugify("Position 1-Candidate 2"), "1")
		req.Form.Add("Position 2", "Candidate 3")
		req.Form.Add("Position 3", "Abstain")
		req.Form.Add("Position 4", "Reopen Nominations")

		resp := httptest.NewRecorder()
		s.mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected StatusOK; got %s", resp.Body.Bytes())
		}
	}

	var voters []Voter
	if err := s.db.Find(&voters).Error; err != nil {
		t.Fatal(err)
	}

	votersWant := []Voter{
		{
			Name:          "Voter",
			StudentNumber: "12345678",
			Username:      "test",
		},
	}
	if len(votersWant) != len(voters) {
		t.Fatalf("voter mismatch!")
	}
	for i, voterGot := range voters {
		voterWant := votersWant[i]
		voterGot.CreatedAt = voterWant.CreatedAt
		voterGot.UpdatedAt = voterWant.UpdatedAt

		if !reflect.DeepEqual(voterWant, voterGot) {
			t.Errorf("%d. %+v != %+v", i, voterWant, voterGot)
		}
	}

	var votes []Vote
	if err := s.db.Find(&votes).Error; err != nil {
		t.Fatal(err)
	}

	sort.Slice(votes, func(i, j int) bool {
		return votes[i].Position < votes[j].Position
	})

	votesWant := []Vote{
		{
			Position:  "Position 1",
			Candidate: `["Candidate 2"]`,
		},
		{
			Position:  "Position 2",
			Candidate: `["Candidate 3"]`,
		},
		{
			Position:  "Position 4",
			Candidate: `["Reopen Nominations"]`,
		},
	}

	if len(votesWant) != len(votes) {
		t.Fatalf("vote mismatch!")
	}
	for i, voteGot := range votes {
		voteWant := votesWant[i]
		voteGot.Model = voteWant.Model

		if !reflect.DeepEqual(voteWant, voteGot) {
			t.Errorf("%d. %+v != %+v", i, voteWant, voteGot)
		}
	}
}
