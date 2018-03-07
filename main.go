package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	mrand "math/rand"
	"net/http"
	"net/http/cgi"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/microcosm-cc/bluemonday"
	"github.com/pkg/errors"

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
	u, _ := user.Current()
	if u != nil {
		fmt.Fprintf(w, "Username: %s\n", u.Username)
		fmt.Fprintf(w, "Name: %s\n", u.Name)
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
			wb.Title("Error")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(500)
			fmt.Fprintf(wb,
				`<h1>Error</h1>
				<p style="color: red">%s</p>
				<p>If the error persists, please contact the elections officer (<a href="mailto:%s">%s</a>).</p>
				<button onclick="window.history.back()">Go Back</button>`,
				err.Error(),
				c.Email, c.Email,
			)
		}
	}
}

type Voter struct {
	Name          string
	Username      string `gorm:"primary_key"`
	StudentNumber string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time `sql:"index"`
}

type Vote struct {
	gorm.Model

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
	Open       bool
	Admins     []string
	DBPath     string
	Email      string
	StudentIDs string
	PrivateKey string
	Bios       []Biography
	Positions  []Position
}

var migrate = flag.Bool("migrate", false, "migrate the database")
var c Config

func main() {
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

	tmpl := template.New("")
	tmpl.Funcs(map[string]interface{}{
		"shuffle": func(src []string) []string {
			dest := make([]string, len(src))
			perm := mrand.Perm(len(src))
			for i, v := range perm {
				dest[v] = src[i]
			}
			return dest
		},
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

	tmpl, err = tmpl.ParseFiles("elections.html", "voted.html", "admin.html")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/vote", handleErr(func(w *TemplateWriter, r *http.Request) error {
		if r.Method != http.MethodPost {
			return errors.New("must use post")
		}
		if !c.Open {
			return errors.New("voting is closed")
		}
		if err := r.ParseForm(); err != nil {
			return err
		}

		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			return errors.New("All fields are required. You need to specify your full name.")
		}
		sid := strings.TrimSpace(r.FormValue("student_number"))
		if sid == "" {
			return errors.New("All fields are required. Student number missing.")
		}
		for _, position := range c.Positions {
			val := r.FormValue(position.Name)
			if val == "" {
				return errors.Errorf("All fields required. Position %q is missing.", position.Name)
			}
			if val == "Abstain" || val == "Reopen Nominations" {
				continue
			}
			found := false
			for _, candidate := range position.Candidates {
				if candidate == val {
					found = true
					break
				}
			}
			if !found {
				return errors.Errorf("invalid candidate %q for position %q", val, position.Name)
			}
		}

		user := os.Getenv("REMOTE_USER")
		if len(user) == 0 {
			return errors.New("missing REMOTE_USER")
		}

		sidsRaw, err := ioutil.ReadFile(c.StudentIDs)
		if err != nil {
			return err
		}
		sids := strings.Split(strings.TrimSpace(string(sidsRaw)), "\n")
		found := false
		for _, sid2 := range sids {
			if sid == strings.TrimSpace(sid2) {
				found = true
				break
			}
		}
		if !found {
			return errors.Errorf("Invalid student number %q. Make sure you typed it in correctly and that you're a computer science student.", sid)
		}

		// We've validated votes, now insert into database.

		tx := db.Begin()

		count := 0
		if err := tx.Model(&Voter{}).Where("username = ? or student_number = ?", user, sid).Count(&count).Error; err != nil {
			return err
		}
		if count >= 1 {
			return errors.Errorf("This user name or student number has already voted.")
		}

		if err := tx.Create(&Voter{
			Username:      user,
			Name:          name,
			StudentNumber: sid,
		}).Error; err != nil {
			return err
		}

		for _, position := range c.Positions {
			val := r.FormValue(position.Name)
			if err := tx.Create(&Vote{
				Position:  position.Name,
				Candidate: val,
			}).Error; err != nil {
				return err
			}
		}

		if err := tx.Commit().Error; err != nil {
			return err
		}

		// Generate cryptographic receipt for votes.
		var body bytes.Buffer
		fmt.Fprintf(&body, "User: %s\n", user)
		for k, v := range r.Form {
			fmt.Fprintf(&body, "- %s: %+v\n", k, v)
		}

		privKey, err := ioutil.ReadFile(c.PrivateKey)
		if err != nil {
			return errors.Wrapf(err, "file %q", c.PrivateKey)
		}
		block, _ := pem.Decode(privKey)
		if block == nil {
			return errors.New("failed to parse PEM block containing the key")
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return err
		}
		hash := sha1.Sum(body.Bytes())
		if err != nil {
			return err
		}
		sig, err := key.Sign(rand.Reader, hash[:], crypto.SHA1)
		if err != nil {
			return err
		}
		body.WriteString("\n")
		body.WriteString(hex.EncodeToString(sig))
		body.WriteString("\n")

		w.Title("Voted")
		return tmpl.ExecuteTemplate(w, "voted.html", body.String())
	}))

	mux.HandleFunc("/debug", debugInfo)

	static := http.FileServer(http.Dir("."))
	mux.Handle("/style.css", static)
	mux.Handle("/scripts.js", static)

	mux.HandleFunc("/admin", handleErr(func(w *TemplateWriter, r *http.Request) error {
		user := os.Getenv("REMOTE_USER")
		if len(user) == 0 {
			return errors.New("missing REMOTE_USER")
		}

		w.Title("Admin")

		found := false
		for _, admin := range c.Admins {
			if admin == user {
				found = true
				break
			}
		}

		if !found {
			return errors.New("must be an admin")
		}

		var body bytes.Buffer
		var voters []Voter
		if err := db.Find(&voters).Error; err != nil {
			return err
		}

		rows, err := db.Model(&Vote{}).Select("position, candidate, count(*)").Group("position, candidate").Order("position DESC, candidate DESC").Rows()
		if err != nil {
			return err
		}

		fmt.Fprintf(&body, "Results\n")
		for rows.Next() {
			var position, candidate string
			var count int
			if err := rows.Scan(&position, &candidate, &count); err != nil {
				return err
			}
			fmt.Fprintf(&body, "- %s - %s: %d\n", position, candidate, count)
		}

		fmt.Fprintf(&body, "Voter count: %d\n", len(voters))
		for _, v := range voters {
			fmt.Fprintf(&body, "- %s, %s, %s\n", v.StudentNumber, v.Name, v.Username)
		}

		return tmpl.ExecuteTemplate(w, "admin.html", body.String())
	}))

	mux.HandleFunc("/", handleErr(func(w *TemplateWriter, r *http.Request) error {
		user := os.Getenv("REMOTE_USER")
		if len(user) == 0 {
			return errors.New("missing REMOTE_USER")
		}

		w.Title("Elections")

		if !c.Open {
			return errors.New("voting is closed")
		}

		return tmpl.ExecuteTemplate(w, "elections.html", struct {
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
