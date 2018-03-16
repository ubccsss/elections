package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	mrand "math/rand"
	"net/http"
	"net/http/cgi"
	"os"
	"os/user"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/Sam-Izdat/govote"
	"github.com/gosimple/slug"
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

type TemplateWriter struct {
	http.ResponseWriter
	writtenHead bool
	footer      []byte
	title       string
}

func (w *TemplateWriter) Title(title string) {
	w.title = title
}

func writeTemplate(w io.Writer, title string) ([]byte, error) {
	body, err := ioutil.ReadFile("template.html")
	if err != nil {
		return nil, err
	}

	seq := []byte("%s")
	body = bytes.Replace(body, seq, []byte(title), 1)

	idx := bytes.Index(body, seq)
	w.Write(body[:idx])
	return body[idx+len(seq):], nil
}

func (w *TemplateWriter) Write(b []byte) (int, error) {
	if !w.writtenHead {
		var err error
		w.footer, err = writeTemplate(w.ResponseWriter, w.title)
		if err != nil {
			return 0, err
		}
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

			log.Printf("Error: %+v", err)
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
	Name      string
	Desc      string
	Image     string
	Positions []string
}

type Position struct {
	Name       string
	Desc       string
	Candidates []string
}

type Config struct {
	Open       bool
	Log        string
	Admins     []string
	DBPath     string
	Email      string
	StudentIDs string
	PrivateKey string
	Bios       []Biography
	Positions  []Position
}

var (
	migrate = flag.Bool("migrate", false, "migrate the database")
	index   = flag.Bool("index", false, "generate index.html")
)
var c Config

func slugify(s string) string {
	return slug.Make(s)
}

func validateVoteForm(r *http.Request) (*Voter, map[string][]string, error) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		return nil, nil, errors.New("All fields are required. You need to specify your full name.")
	}
	sid := strings.TrimSpace(r.FormValue("student_number"))
	if sid == "" {
		return nil, nil, errors.New("All fields are required. Student number missing.")
	}

	type rank struct {
		rank   int
		choice string
	}

	positionChoices := map[string][]string{}

	for _, position := range c.Positions {
		if len(position.Candidates) == 0 {
			continue
		}

		val := r.FormValue(position.Name)
		var ranks []rank
		if val == "Abstain" {
			continue
		}
		if val == "Reopen Nominations" {
			ranks = append(ranks, rank{
				rank:   0,
				choice: val,
			})
		}
		for _, candidate := range position.Candidates {
			if candidate == val {
				ranks = append(ranks, rank{
					rank:   0,
					choice: candidate,
				})
				continue
			}
			id := slugify(position.Name + "-" + candidate)
			candidateVal := r.FormValue(id)
			if candidateVal != "" {
				r, err := strconv.Atoi(candidateVal)
				if err != nil {
					return nil, nil, err
				}
				ranks = append(ranks, rank{
					rank:   r,
					choice: candidate,
				})
			}
		}
		if len(ranks) == 0 {
			return nil, nil, errors.Errorf("All fields are required. Invalid or missing position %q.", position.Name)
		}

		sort.Slice(ranks, func(i, j int) bool {
			return ranks[i].rank < ranks[j].rank
		})
		var choices []string
		for i, r := range ranks {
			if i > 0 && ranks[i-1].rank == r.rank {
				return nil, nil, errors.Errorf("ranks must be unique: %s duplicate rank %d", position.Name, r.rank)
			}
			choices = append(choices, r.choice)
		}
		positionChoices[position.Name] = choices
	}

	user := os.Getenv("REMOTE_USER")
	if len(user) == 0 {
		return nil, nil, errors.New("missing REMOTE_USER")
	}

	sidsRaw, err := ioutil.ReadFile(c.StudentIDs)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, errors.Errorf("Invalid student number %q. Make sure you typed it in correctly and that you're a computer science student.", sid)
	}

	voter := &Voter{
		Username:      user,
		Name:          name,
		StudentNumber: sid,
	}
	return voter, positionChoices, nil
}

func runMigrate(db *gorm.DB) error {
	db.AutoMigrate(&Voter{})
	db.AutoMigrate(&Vote{})
	return nil
}

type server struct {
	db  *gorm.DB
	mux *http.ServeMux
}

func (s *server) Close() error {
	return s.db.Close()
}

func isAdmin(user string) bool {
	for _, admin := range c.Admins {
		if admin == user {
			return true
		}
	}
	return false
}

func setup() (*server, error) {
	flag.Parse()

	positions := map[string][]string{}
	for _, p := range c.Positions {
		for _, c := range p.Candidates {
			positions[c] = append(positions[c], p.Name)
		}
	}
	for i, b := range c.Bios {
		c.Bios[i].Positions = positions[b.Name]
	}

	if len(c.DBPath) == 0 {
		return nil, errors.Errorf("dbpath empty!")
	}

	tmpl := template.New("")
	tmpl.Funcs(map[string]interface{}{
		"shuffle": func(src interface{}) interface{} {
			srcR := reflect.ValueOf(src)
			l := srcR.Len()
			dest := reflect.MakeSlice(srcR.Type(), l, l)
			perm := mrand.Perm(l)
			for i, v := range perm {
				dest.Index(v).Set(srcR.Index(i))
			}
			return dest.Interface()
		},
		"concat": func(a ...string) string {
			return strings.Join(a, "")
		},
		"slug": slugify,
		"md": func(s string) interface{} {
			unsafe := blackfriday.Run([]byte(s))
			sanitized := bluemonday.UGCPolicy().SanitizeBytes(unsafe)
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader(sanitized))
			if err != nil {
				return errors.Wrapf(err, "md: %q", s)
			}
			doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
				s.SetAttr("target", "_blank")
			})
			html, err := goquery.OuterHtml(doc.Selection)
			if err != nil {
				return errors.Wrapf(err, "md: %q", s)
			}
			return template.HTML(html)
		},
		"seq": func(n int) []int {
			var nums []int
			for i := 1; i <= n; i++ {
				nums = append(nums, i)
			}
			return nums
		},
		"hasBio": func(candidate string) bool {
			for _, b := range c.Bios {
				if b.Name == candidate {
					return true
				}
			}
			return false
		},
	})

	tmpl, err := tmpl.ParseGlob("templates/*")
	if err != nil {
		return nil, err
	}

	if *index {
		f, err := os.OpenFile("index.html", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		footer, err := writeTemplate(f, "Elections")
		if err != nil {
			return nil, err
		}
		if err := tmpl.ExecuteTemplate(f, "index.html", nil); err != nil {
			return nil, err
		}
		if _, err := f.Write(footer); err != nil {
			return nil, err
		}

		return nil, nil
	}

	// NOTE: this database has to be able to be opened and edited by multiple
	// clients at the same time since this is a CGI based program and there may
	// be n copies operating at the same time.
	db, err := gorm.Open("sqlite3", c.DBPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to database")
	}

	if *migrate {
		if err := runMigrate(db); err != nil {
			return nil, err
		}
		return nil, nil
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

		voter, positionChoices, err := validateVoteForm(r)
		if err != nil {
			return err
		}

		// We've validated votes, now insert into database.

		tx := db.Begin()

		count := 0
		if err := tx.Model(&Voter{}).Where("username = ? or student_number = ?", voter.Username, voter.StudentNumber).Count(&count).Error; err != nil {
			return err
		}
		if count >= 1 {
			return errors.Errorf("This user name or student number has already voted.")
		}

		if err := tx.Create(&voter).Error; err != nil {
			return err
		}

		for position, choices := range positionChoices {
			jsonChoices, err := json.Marshal(choices)
			if err != nil {
				return err
			}
			if err := tx.Create(&Vote{
				Position:  position,
				Candidate: string(jsonChoices),
			}).Error; err != nil {
				return err
			}
		}

		if err := tx.Commit().Error; err != nil {
			return err
		}

		// Generate cryptographic receipt for votes.
		var body bytes.Buffer
		fmt.Fprintf(&body, "User: %+v\n", voter)
		fmt.Fprintf(&body, "Time: %s\n", time.Now().String())
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
		body.WriteString(base64.StdEncoding.EncodeToString(sig))
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

		if !isAdmin(user) {
			return errors.New("must be an admin")
		}

		var body bytes.Buffer
		var voters []Voter
		if err := db.Find(&voters).Error; err != nil {
			return err
		}

		polls := map[string]*govote.InstantRunoffPoll{}
		for _, position := range c.Positions {
			candidates := append(position.Candidates, "Reopen Nominations")
			if len(candidates) < 2 {
				candidates = append(candidates, "no one")
			}
			poll, err := govote.InstantRunoff.New(candidates)
			if err != nil {
				return errors.Wrapf(err, "position %+v; candidates %+v", position, candidates)
			}
			polls[position.Name] = &poll
		}

		var votes []Vote
		if err := db.Find(&votes).Error; err != nil {
			return err
		}

		for _, v := range votes {
			var candidates []string
			if err := json.Unmarshal([]byte(v.Candidate), &candidates); err != nil {
				return err
			}
			if !polls[v.Position].AddBallot(candidates) {
				fmt.Fprintf(&body, "error: Failed to AddBallot for vote: %#v\n", v)
			}
		}

		fmt.Fprintf(&body, "Results:\n")
		for _, p := range c.Positions {
			poll := polls[p.Name]
			winners, rounds, err := poll.Evaluate()
			if err != nil {
				fmt.Fprintf(&body, "- %s:\n  error: %+v\n", p.Name, err)
			} else {
				fmt.Fprintf(&body, "- %s:\n  Winner: %s\n", p.Name, strings.Join(winners, ","))
				for i, round := range rounds {
					fmt.Fprintf(&body, "  - Round %d:\n", i)
					for _, p := range round {
						fmt.Fprintf(&body, "    - %+v\n", p)
					}
				}
			}
		}

		fmt.Fprintf(&body, "\nVoter count: %d\nVoters:\n", len(voters))
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

		if !c.Open && !isAdmin(user) {
			return errors.New("voting is closed")
		}

		count := 0
		if err := db.Model(&Voter{}).Where("username = ?", user).Count(&count).Error; err != nil {
			return err
		}

		return tmpl.ExecuteTemplate(w, "elections.html", struct {
			Config
			User  string
			Voted bool
		}{
			Config: c,
			User:   user,
			Voted:  count > 0,
		})
	}))

	return &server{
		mux: mux,
		db:  db,
	}, nil
}

func main() {

	mrand.Seed(time.Now().UnixNano())

	rawConfig, err := ioutil.ReadFile("config.yml")
	if err != nil {
		log.Fatal(err)
	}

	if err := yaml.Unmarshal(rawConfig, &c); err != nil {
		log.Fatal(err)
	}

	writer := io.Writer(os.Stderr)

	if len(c.Log) > 0 {
		logfile, err := os.OpenFile(c.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Println(err)
		} else {
			defer logfile.Close()

			writer = io.MultiWriter(writer, logfile)
		}
	}

	log.SetOutput(writer)

	server, err := setup()
	if err != nil {
		log.Fatal(err)
	}

	if server == nil {
		return
	}
	defer server.Close()

	if err := cgi.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bits := strings.Split(r.URL.Path, "elections.cgi")
		if len(bits) == 2 {
			r.URL.Path = bits[1]
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}
		log.Printf("%s %s", r.Method, r.URL.Path)
		server.mux.ServeHTTP(w, r)
	})); err != nil {
		log.Println(err)
	}
}
