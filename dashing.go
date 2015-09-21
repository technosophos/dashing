package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"text/template"

	css "github.com/andybalholm/cascadia"
	"github.com/codegangsta/cli"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	_ "github.com/mattn/go-sqlite3"
)

const plist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>{{.Name}}</string>
	<key>CFBundleName</key>
	<string>{{.FancyName}}</string>
	<key>DocSetPlatformFamily</key>
	<string>{{.Name}}</string>
	<key>isDashDocset</key>
	<true/>
	<key>DashDocSetFamily</key>
	<string>dashtoc</string>
	<key>dashIndexFilePath</key>
	<string>{{.Index}}</string>
	<key>isJavaScriptEnabled</key><{{.AllowJS}}/>
</dict>
</plist>
`

// Automatically replaced by linker.
var version = "dev"

type Dashing struct {
	// The human-oriented name of the package.
	Name string `json:"name"`
	// Computer-readable name. Recommendation is to use one word.
	Package string `json:"package"`
	// The location of the index.html file.
	Index string `json:"index"`
	// Selectors to match.
	Selectors map[string]interface{} `json:"selectors"`
	// Final form of the Selectors field.
	selectors map[string]*Transform `json:"-"`
	// Entries that should be ignored.
	Ignore []string `json:"ignore"`
	// A 32x32 pixel PNG image.
	Icon32x32 string `json:"icon32x32"`
	AllowJS   bool   `json:"allowJS"`
}

// Transform is a description of what should be done with a selector.
// When the Selectors map is unmarshaled, the values are turned into
// Transform structs.
type Transform struct {
	Type        string
	Regexp      *regexp.Regexp
	Replacement string
}

var ignoreHash map[string]bool

func main() {
	app := cli.NewApp()
	app.Name = "dashing"
	app.Usage = "Generate Dash documentation from HTML files"
	app.Version = version

	app.Commands = commands()

	app.Run(os.Args)
}

func commands() []cli.Command {
	return []cli.Command{
		{
			Name:   "build",
			Usage:  "build a doc set",
			Action: build,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "source, s",
					Usage: "The path to the HTML files this will ingest. (Default: ./ )",
				},
				cli.StringFlag{
					Name:  "config, f",
					Usage: "The path to the JSON configuration file.",
				},
			},
		},
		{
			Name:      "init",
			ShortName: "create",
			Usage:     "create a new template for building documentation",
			Action:    create,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config, f",
					Usage: "The path to the JSON configuration file.",
				},
			},
		},
		{
			Name:   "version",
			Usage:  "Print version and exit.",
			Action: func(c *cli.Context) { fmt.Println(version) },
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config, f",
					Usage: "The path to the JSON configuration file.",
				},
			},
		},
	}
}

func create(c *cli.Context) {
	f := c.String("config")
	if len(f) == 0 {
		f = "dashing.json"
	}
	conf := Dashing{
		Name:    "Dashing",
		Package: "dashing",
		Index:   "index.html",
		Selectors: map[string]interface{}{
			"title": "Package",
			"dt a":  "Command",
		},
		Ignore: []string{"ABOUT"},
	}
	j, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		panic("The programmer did something dumb.")
	}
	err = ioutil.WriteFile(f, j, 0755)
	if err != nil {
		fmt.Errorf("Could not initialize JSON file: %s", err)
		os.Exit(1)
	}
	fmt.Printf("You may now edit %s", f)

}

func build(c *cli.Context) {
	var dashing Dashing

	source := c.String("source")
	if len(source) == 0 {
		source = "."
	}

	cf := strings.TrimSpace(c.String("config"))
	if len(cf) == 0 {
		cf = "./dashing.json"
	}

	conf, err := ioutil.ReadFile(cf)
	if err != nil {
		fmt.Printf("Failed to open configuration file '%s': %s (Run `dashing init`?)\n", cf, err)
		os.Exit(1)
	}

	if err := json.Unmarshal(conf, &dashing); err != nil {
		fmt.Printf("Failed to parse JSON: %s", err)
		os.Exit(1)
	}
	if err := decodeSelectField(&dashing); err != nil {
		fmt.Printf("Could not understand selector value: %s\n", err)
		os.Exit(2)
	}

	name := dashing.Package

	fmt.Printf("Building %s from files in '%s'.\n", name, source)

	os.MkdirAll(name+".docset/Contents/Resources/Documents/", 0755)

	setIgnore(dashing.Ignore)
	addPlist(name, &dashing)
	if len(dashing.Icon32x32) > 0 {
		addIcon(dashing.Icon32x32, name+".docset/icon.png")
	}
	db, err := createDB(name)
	if err != nil {
		fmt.Printf("Failed to create database: %s\n", err)
		return
	}
	defer db.Close()
	texasRanger(source, name, dashing, db)
}

func decodeSelectField(d *Dashing) error {
	d.selectors = make(map[string]*Transform, len(d.Selectors))
	for sel, val := range d.Selectors {
		var trans *Transform
		rv := reflect.Indirect(reflect.ValueOf(val))
		if rv.Kind() == reflect.String {
			trans = &Transform{
				Type: val.(string),
			}
		} else if rv.Kind() == reflect.Map {
			val := val.(map[string]interface{})
			var ttype, treg, trep string
			if t, ok := val["type"]; ok {
				ttype = t.(string)
			}
			if r, ok := val["regexp"]; ok {
				treg = r.(string)
			}
			if r, ok := val["replacement"]; ok {
				trep = r.(string)
			}
			var creg *regexp.Regexp
			var err error
			if len(treg) > 0 {
				if creg, err = regexp.Compile(treg); err != nil {
					return fmt.Errorf("failed to compile regexp '%s': %s", treg, err)
				}
			}
			trans = &Transform{
				Type:        ttype,
				Regexp:      creg,
				Replacement: trep,
			}
		} else {
			fmt.Errorf("Expected string or map. Kind is %s.", rv.Kind().String())
		}
		d.selectors[sel] = trans
	}
	return nil
}

func setIgnore(i []string) {
	ignoreHash = make(map[string]bool, len(i))
	for _, item := range i {
		ignoreHash[item] = true
	}
}

func addPlist(name string, config *Dashing) {
	var file bytes.Buffer
	t := template.Must(template.New("plist").Parse(plist))

	fancyName := config.Name
	if len(fancyName) == 0 {
		fancyName = strings.ToTitle(name)
	}

	aj := "false"
	if config.AllowJS {
		aj = "true"
	}

	tvars := map[string]string{
		"Name":      name,
		"FancyName": fancyName,
		"Index":     config.Index,
		"AllowJS":   aj,
	}

	err := t.Execute(&file, tvars)
	if err != nil {
		fmt.Printf("Failed: %s\n", err)
		return
	}
	ioutil.WriteFile(name+".docset/Contents/Info.plist", file.Bytes(), 0755)
}

func createDB(name string) (*sql.DB, error) {
	dbname := name + ".docset/Contents/Resources/docSet.dsidx"
	os.Remove(dbname)

	db, err := sql.Open("sqlite3", dbname)
	if err != nil {
		return db, err
	}
	if _, err := db.Exec(`CREATE TABLE searchIndex(id INTEGER PRIMARY KEY, name TEXT, type TEXT, path TEXT)`); err != nil {
		return db, err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX anchor ON searchIndex (name, type, path)`); err != nil {
		return db, err
	}
	return db, nil
}

// texasRanger is... wait for it... a WALKER!
func texasRanger(base, name string, dashing Dashing, db *sql.DB) error {
	filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		fmt.Printf("Reading %s\n", path)
		if strings.HasPrefix(path, name+".docset") {
			fmt.Printf("Ignoring directory %s\n", path)
			return filepath.SkipDir
		}
		if info.IsDir() || ignore(path) {
			return nil
		}
		dest := name + ".docset/Contents/Resources/Documents"
		if htmlish(path) {
			fmt.Printf("%s looks like HTML\n", path)
			//if err := copyFile(path, name+".docset/Contents/Resources/Documents"); err != nil {
			//fmt.Printf("Failed to copy file %s: %s\n", path, err)
			//return err
			//}
			found, err := parseHTML(path, dest, dashing)
			if err != nil {
				fmt.Printf("Error parsing %s: %s\n", path, err)
				return nil
			}
			for _, ref := range found {
				fmt.Printf("Match: '%s' is type %s at %s\n", ref.name, ref.etype, ref.href)
				db.Exec(`INSERT OR IGNORE INTO searchIndex(name, type, path) VALUES (?,?,?)`, ref.name, ref.etype, ref.href)
			}
		} else {
			// Or we just copy the file.
			err := copyFile(path, filepath.Join(dest, path))
			if err != nil {
				fmt.Printf("Skipping file %s. Error: %s\n", path, err)
			}
			return err
		}
		return nil
	})
	return nil
}

// ignore returns true if a file should be ignored by dashing.
func ignore(src string) bool {

	// Skip our own config file.
	if filepath.Base(src) == "dashing.json" {
		return true
	}

	// Skip VCS dirs.
	parts := strings.Split(src, "/")
	for _, p := range parts {
		switch p {
		case ".git", ".svn":
			return true
		}
	}
	return false
}

func writeHTML(orig, dest string, root *html.Node) error {
	dir := filepath.Dir(orig)
	base := filepath.Base(orig)
	os.MkdirAll(filepath.Join(dest, dir), 0755)
	out, err := os.Create(filepath.Join(dest, dir, base))
	if err != nil {
		return err
	}
	defer out.Close()

	return html.Render(out, root)
}

func htmlish(filename string) bool {
	e := strings.ToLower(filepath.Ext(filename))
	switch e {
	case ".html", ".htm", ".xhtml", ".html5":
		return true
	}
	return false
}

type reference struct {
	name, etype, href string
}

func parseHTML(path, dest string, dashing Dashing) ([]*reference, error) {
	refs := []*reference{}

	r, err := os.Open(path)
	if err != nil {
		return refs, err
	}
	defer r.Close()
	top, err := html.Parse(r)

	for pattern, sel := range dashing.selectors {
		m := css.MustCompile(pattern)
		found := m.MatchAll(top)
		for _, n := range found {
			name := text(n)

			// Skip things explicitly ignored.
			if ignored(name) {
				fmt.Printf("Skipping entry for %s (Ignored by dashing JSON)\n", name)
				continue
			}

			// If we have a regexp, run it.
			if sel.Regexp != nil {
				name = sel.Regexp.ReplaceAllString(name, sel.Replacement)
			}

			// References we want to track.
			refs = append(refs, &reference{name, sel.Type, path + "#" + anchor(n)})
			// We need to modify the DOM with a special link to support TOC.
			n.Parent.InsertBefore(newA(name, sel.Type), n)
		}
	}
	return refs, writeHTML(path, dest, top)
}

func ignored(n string) bool {
	_, ok := ignoreHash[n]
	return ok
}

func text(node *html.Node) string {
	var b bytes.Buffer
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		} else if c.Type == html.ElementNode {
			b.WriteString(text(c))
		}
	}
	return strings.TrimSpace(b.String())
}

// tcounter is used to generate automatic anchors.
// NOTE: NOT THREADSAFE. If we switch to goroutines, swith to atom int.
var tcounter int

func anchor(node *html.Node) string {
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, a := range node.Attr {
			if a.Key == "name" {
				return a.Val
			}
		}
	}
	tname := fmt.Sprintf("autolink-%d", tcounter)
	link := autolink(tname)
	node.Parent.InsertBefore(link, node)
	tcounter++
	return tname
}

//autolink creates an A tag for when one is not present in original docs.
func autolink(target string) *html.Node {
	return &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.A,
		Data:     atom.A.String(),
		Attr: []html.Attribute{
			html.Attribute{Key: "class", Val: "dashingAutolink"},
			html.Attribute{Key: "name", Val: target},
		},
	}
}

// newA creates a TOC anchor.
func newA(name, etype string) *html.Node {
	name = strings.Replace(url.QueryEscape(name), "+", "%20", -1)

	target := fmt.Sprintf("//apple_ref/cpp/%s/%s", etype, name)
	return &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.A,
		Data:     atom.A.String(),
		Attr: []html.Attribute{
			html.Attribute{Key: "class", Val: "dashAnchor"},
			html.Attribute{Key: "name", Val: target},
		},
	}
}

// addIcon adds an icon to the docset.
func addIcon(src, dest string) error {
	return copyFile(src, dest)
}

// copyFile copies a source file to a new destination.
func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
