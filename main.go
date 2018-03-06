package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cgi"
	"os"
	"strings"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/microcosm-cc/bluemonday"

	blackfriday "gopkg.in/russross/blackfriday.v2"
	yaml "gopkg.in/yaml.v2"
)

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

type TemplateWriter struct {
	http.ResponseWriter
	writtenHead bool
	footer      []byte
	title       string
}

func (w *TemplateWriter) Title(title string) {
	w.title = title
}

func (w *TemplateWriter) Write(b []byte) (int, error) {
	if !w.writtenHead {
		body, err := ioutil.ReadFile("template.html")
		if err != nil {
			return 0, err
		}

		seq := []byte("%s")
		body = bytes.Replace(body, seq, []byte(w.title), 1)

		idx := bytes.Index(body, seq)
		w.ResponseWriter.Write(body[:idx])
		w.footer = body[idx+len(seq):]
		w.writtenHead = true
	}

	return w.ResponseWriter.Write(b)
}

func (w *TemplateWriter) Close() error {
	if _, err := w.ResponseWriter.Write(w.footer); err != nil {
		return err
	}
	return nil
}

func handleErr(f func(w *TemplateWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		wb := &TemplateWriter{ResponseWriter: w}
		defer wb.Close()

		if err := f(wb, r); err != nil {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(500)
			fmt.Fprintf(wb,
				`<h1>Error</h1>
				<p style="color: red">%s</p>
				<button onclick="window.history.back()">Go Back</button>`,
				err.Error(),
			)
		}
	}
}

type Voter struct {
	Name          string
	Username      string
	StudentNumber string
}

type Vote struct {
	Position  string
	Candidate string
}

type Biography struct {
	Name string
	Desc string
}

type Position struct {
	Name       string
	Desc       string
	Candidates []string
}

type Config struct {
	DBPath    string
	Email     string
	Bios      []Biography
	Positions []Position
}

var migrate = flag.Bool("migrate", false, "migrate the database")

func main() {
	var c Config
	rawConfig, err := ioutil.ReadFile("config.yml")
	if err != nil {
		log.Fatal(err)
	}

	if err := yaml.Unmarshal(rawConfig, &c); err != nil {
		log.Fatal(err)
	}

	if len(c.DBPath) == 0 {
		log.Fatal("dbpath empty!")
	}

	// NOTE: this database has to be able to be opened and edited by multiple
	// clients at the same time since this is a CGI based program and there may
	// be n copies operating at the same time.
	db, err := gorm.Open("sqlite3", c.DBPath)
	if err != nil {
		log.Fatal("failed to connect database")
	}
	defer db.Close()

	flag.Parse()
	if *migrate {
		db.AutoMigrate(&Voter{})
		db.AutoMigrate(&Vote{})
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/vote", handleErr(func(w *TemplateWriter, r *http.Request) error {
		http.ServeFile(w, r, "voted.html")
		return nil
	}))

	mux.HandleFunc("/debug", debugInfo)

	static := http.FileServer(http.Dir("."))
	mux.Handle("/style.css", static)
	mux.Handle("/scripts.js", static)

	mux.HandleFunc("/", handleErr(func(w *TemplateWriter, r *http.Request) error {
		user := os.Getenv("REMOTE_USER")
		if len(user) == 0 {
			return errors.New("missing REMOTE_USER")
		}

		tmpl := template.New("elections.html")
		tmpl.Funcs(map[string]interface{}{
			"concat": func(a, b string) string {
				return a + b
			},
			"slug": func(s string) string {
				return strings.ToLower(strings.Replace(s, " ", "-", -1))
			},
			"md": func(s string) template.HTML {
				unsafe := blackfriday.Run([]byte(s))
				return template.HTML(string(bluemonday.UGCPolicy().SanitizeBytes(unsafe)))
			},
		})

		tmpl, err := tmpl.ParseFiles("elections.html")
		if err != nil {
			return err
		}
		return tmpl.Execute(w, struct {
			Config
			User string
		}{
			Config: c,
			User:   user,
		})
	}))

	if err := cgi.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bits := strings.Split(r.URL.Path, "elections.cgi")
		if len(bits) == 2 {
			r.URL.Path = bits[1]
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}
		mux.ServeHTTP(w, r)
	})); err != nil {
		fmt.Println(err)
	}
}
