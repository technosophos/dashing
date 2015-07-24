package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	css "github.com/andybalholm/cascadia"
	"github.com/codegangsta/cli"
	"golang.org/x/net/html"

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
</dict>
</plist>
`

type Dashing struct {
	Name      string            `json:"name"`
	Selectors map[string]string `json:"selectors"`
	Ignore    []string          `json:"ignore"`
}

func main() {
	app := cli.NewApp()
	app.Name = "dashing"
	app.Usage = "Generate Dash documentation from HTML files"

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
			Name:   "init",
			Usage:  "create a new template for building documentation",
			Action: create,
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
		Name: "Dashing",
		Selectors: map[string]string{
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

	if !c.Args().Present() {
		fmt.Printf("Name is required: dashing build NAME\n")
		return
	}
	name := c.Args().First()
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

	fmt.Printf("Building %s from files in '%s'.\n", name, source)

	os.MkdirAll(name+".docset/Contents/Resources/Documents/", 0755)

	addPlist(name)
	db, err := createDB(name)
	if err != nil {
		fmt.Printf("Failed to create database: %s\n", err)
		return
	}
	defer db.Close()
	texasRanger(source, name, dashing, db)

}

func addPlist(name string) {
	var file bytes.Buffer
	t := template.Must(template.New("plist").Parse(plist))

	tvars := map[string]string{
		"Name":      name,
		"FancyName": strings.ToTitle(name),
	}

	err := t.Execute(&file, tvars)
	if err != nil {
		fmt.Printf("Failed: %s\n", err)
		return
	}
	ioutil.WriteFile(name+".docset/Contents/Info.plist", file.Bytes(), 0755)
}

func createDB(name string) (*sql.DB, error) {
	dbname := name + ".docset/Contents/Resources/docset.dxidx"
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
		if strings.HasPrefix(path, name+".docset") {
			return filepath.SkipDir
		}
		//fmt.Printf("Walking %s\n", path)
		if !info.IsDir() && htmlish(path) {
			fmt.Printf("%s looks like HTML\n", path)
			if err := copyFile(path, name+".docset/Contents/Resources/Documents"); err != nil {
				fmt.Printf("Failed to copy file %s: %s\n", path, err)
				return err
			}
			found, _ := parseHTML(path, dashing)
			for _, ref := range found {
				fmt.Printf("Match: '%s' is type %s at %s\n", ref.name, ref.etype, ref.href)
				db.Exec(`INSERT OR IGNORE INTO searchIndex(name, type, path) VALUES (?,?,?)`, ref.name, ref.etype, ref.href)
			}
		}
		return nil
	})
	return nil
}

func copyFile(orig, dest string) error {
	dir := filepath.Dir(orig)
	base := filepath.Base(orig)
	os.MkdirAll(filepath.Join(dest, dir), 0755)
	in, err := os.Open(orig)
	if err != nil {
		return err
	}
	out, _ := os.Create(filepath.Join(dest, dir, base))
	if err != nil {
		in.Close()
		return err
	}

	io.Copy(out, in)
	in.Close()
	return out.Close()
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

func parseHTML(path string, dashing Dashing) ([]*reference, error) {
	refs := []*reference{}

	r, err := os.Open(path)
	if err != nil {
		return refs, err
	}
	defer r.Close()
	top, err := html.Parse(r)

	for pattern, etype := range dashing.Selectors {
		m := css.MustCompile(pattern)
		found := m.MatchAll(top)
		for _, n := range found {
			refs = append(refs, &reference{text(n), etype, anchor(n)})
		}
	}

	return refs, nil
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
	return b.String()
}

func anchor(node *html.Node) string {
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, a := range node.Attr {
			if a.Key == "name" {
				return a.Val
			}
		}
	}
	return ""
}
