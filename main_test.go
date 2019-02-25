package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func TestNoAllow(t *testing.T) {
	config := map[string]Cmd{
		"/hello": Cmd{
			Command: "echo",
			Args:    []string{"hello", "world"},
		},
		"/hello/2": Cmd{
			Command: "echo",
			Args:    []string{"hello world"},
		},
	}

	server := httptest.NewServer(handlerFunc(config))
	defer server.Close()

	resp := req(t, server.URL+"/invalid", nil)
	expectStatus(t, resp, 404)
	expectResult(t, resp, Result{Output: "", Error: "handler not found", Status: 404})

	resp = req(t, server.URL+"/hello", nil)
	expectStatus(t, resp, 200)
	expectResult(t, resp, Result{Output: "hello world\n", Error: "", Status: 200})

	resp = req(t, server.URL+"/hello/2", nil)
	expectStatus(t, resp, 200)
	expectResult(t, resp, Result{Output: "hello world\n", Error: "", Status: 200})

	// Test params when nothing is allowed.
	resp = req(t, server.URL+"/hello/2", bytes.NewBuffer([]byte(`{"args": []}`)))
	expectStatus(t, resp, 200)
	resp = req(t, server.URL+"/hello/2", bytes.NewBuffer([]byte(`{x}`)))
	expectStatus(t, resp, 400)
	resp = req(t, server.URL+"/hello/2", bytes.NewBuffer([]byte(`{"args": ["hello"]}`)))
	expectStatus(t, resp, 403)
	expectResult(t, resp, Result{Output: "", Error: "args param is not allowed", Status: 403})
	resp = req(t, server.URL+"/hello/2", bytes.NewBuffer([]byte(`{"envs": ["FOO=BAR"]}`)))
	expectStatus(t, resp, 403)
	expectResult(t, resp, Result{Output: "", Error: "envs param is not allowed", Status: 403})
}

func TestAllowArgs(t *testing.T) {
	config := map[string]Cmd{
		"/hello": Cmd{
			Command: "echo",
			Args:    []string{"hello"},
			Allow:   []string{"args"},
		},
	}

	server := httptest.NewServer(handlerFunc(config))
	defer server.Close()

	resp := req(t, server.URL+"/hello", bytes.NewBuffer([]byte(`{"args": ["world"]}`)))
	expectStatus(t, resp, 200)
	expectResult(t, resp, Result{
		Output: "hello world\n",
		Error:  "",
		Status: 200,
	})
}

func TestAllowEnvs(t *testing.T) {
	config := map[string]Cmd{
		"/env": Cmd{
			Command: "env",
			Envs:    []string{"FOO=BAR"},
			Allow:   []string{"envs"},
		},
	}

	server := httptest.NewServer(handlerFunc(config))
	defer server.Close()

	resp := req(t, server.URL+"/env", bytes.NewBuffer([]byte(`{"envs": ["BAR=BAZ"]}`)))
	expectStatus(t, resp, 200)
	expectResult(t, resp, Result{
		Output: "FOO=BAR\nBAR=BAZ\n",
		Error:  "",
		Status: 200,
	})
}

func TestAllowStdin(t *testing.T) {
	tmp, err := ioutil.TempFile("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write([]byte("foo")); err != nil {
		t.Fatal(err)
	}

	config := map[string]Cmd{
		"/diff": Cmd{
			Command: "diff",
			Args:    []string{tmp.Name(), "-"},
			Allow:   []string{"stdin"},
		},
	}

	server := httptest.NewServer(handlerFunc(config))
	defer server.Close()

	resp := req(t, server.URL+"/diff", bytes.NewBuffer([]byte(`{"stdin": "foo"}`)))
	expectStatus(t, resp, 200)
	expectResult(t, resp, Result{
		Output: "",
		Error:  "",
		Status: 200,
	})
}

func req(t *testing.T, url string, body io.Reader) *http.Response {
	resp, err := http.Post(url, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func expectStatus(t *testing.T, resp *http.Response, expected int) {
	if resp.StatusCode != expected {
		t.Fatalf("Expect status %d, but got %d", expected, resp.StatusCode)
	}
}

func expectResult(t *testing.T, resp *http.Response, expected Result) {
	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	result.RequestID = ""
	if !reflect.DeepEqual(expected, result) {
		t.Fatalf("Expect result %q, but got %q", expected, result)
	}
}
