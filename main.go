package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cgi"
	"os"
	"strings"
)

func handleError(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, err.Error(), 500)

	debugInfo(w, r)
}

func debugInfo(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "<h2>Debug Info</h2>")
	w.Write([]byte("<pre>"))
	fmt.Fprintln(w, "Method:", r.Method)
	fmt.Fprintln(w, "URL:", r.URL.String())
	query := r.URL.Query()
	for k := range query {
		fmt.Fprintln(w, "Query", k+":", query.Get(k))
	}
	r.ParseForm()
	form := r.Form
	for k := range form {
		fmt.Fprintln(w, "Form", k+":", form.Get(k))
	}
	post := r.PostForm
	for k := range post {
		fmt.Fprintln(w, "PostForm", k+":", post.Get(k))
	}
	fmt.Fprintln(w, "RemoteAddr:", r.RemoteAddr)
	if referer := r.Referer(); len(referer) > 0 {
		fmt.Fprintln(w, "Referer:", referer)
	}
	if ua := r.UserAgent(); len(ua) > 0 {
		fmt.Fprintln(w, "UserAgent:", ua)
	}
	for _, cookie := range r.Cookies() {
		fmt.Fprintln(w, "Cookie", cookie.Name+":", cookie.Value, cookie.Path, cookie.Domain, cookie.RawExpires)
	}
	for k, v := range r.Header {
		fmt.Fprintln(w, "Header", k+": "+strings.Join(v, ", "))
	}
	if user, pass, ok := r.BasicAuth(); ok {
		fmt.Fprintf(w, "User: %s, Pass: %s, OK: %v\n", user, pass, ok)
	}
	for _, env := range os.Environ() {
		fmt.Fprintf(w, "Env: %s\n", env)
	}
	w.Write([]byte("</pre>"))
}

const voteFile = "votes.json"

func loadVotes() (map[string]map[string]int, error) {
	m := map[string]map[string]int{}
	file, err := ioutil.ReadFile(voteFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(file, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveVotes(m map[string]map[string]int) error {
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(voteFile, buf, 0755); err != nil {
		return err
	}
	return nil
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/~q7w9a/elections/elections.cgi/vote", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<h1>Thanks for voting!</h1>")

		votes, err := loadVotes()
		if err != nil {
			handleError(w, r, err)
			return
		}

		r.ParseForm()
		for k := range r.PostForm {
			people, ok := votes[k]
			if !ok {
				people = make(map[string]int)
				votes[k] = people
			}
			people[r.PostForm.Get(k)]++
		}

		if err := saveVotes(votes); err != nil {
			handleError(w, r, err)
			return
		}

		fmt.Fprintf(w, "<p>Votes: <pre>%#v</pre></p>", votes)

		debugInfo(w, r)
	})

	mux.HandleFunc("/~q7w9a/elections/elections.cgi/", func(w http.ResponseWriter, r *http.Request) {
		user := os.Getenv("REMOTE_USER")
		if len(user) == 0 {
			http.Error(w, "missing REMOTE_USER", 400)
			return
		}
		fmt.Fprintf(w, "<h1>Welcome, %s</h1>", user)

		form, err := ioutil.ReadFile("elections.html")
		if err != nil {
			handleError(w, r, err)
			return
		}
		w.Write(form)

		debugInfo(w, r)
	})

	if err := cgi.Serve(mux); err != nil {
		fmt.Println(err)
	}
}
