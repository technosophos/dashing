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
	"github.com/urfave/cli"
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
	<key>isJavaScriptEnabled</key><{{.AllowJS}}/>{{if .ExternalURL}}
	<key>DashDocSetFallbackURL</key>
	<string>{{.ExternalURL}}</string>{{end}}
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
	selectors map[string][]*Transform `json:"-"`
	// Entries that should be ignored.
	Ignore []string `json:"ignore"`
	// A 32x32 pixel PNG image.
	Icon32x32 string `json:"icon32x32"`
	AllowJS   bool   `json:"allowJS"`
	// External URL for "Open Online Page"
	ExternalURL string `json: "externalURL"`
}

// Transform is a description of what should be done with a selector.
// When the Selectors map is unmarshaled, the values are turned into
// Transform structs.
type Transform struct {
	Type        string
	Attribute   string         // Use the value of this attribute as basis
	Regexp      *regexp.Regexp // Perform a replace operation on the text
	Replacement string
	RequireText *regexp.Regexp // Require text matches the given regexp
	MatchPath   *regexp.Regexp // Skip files that don't match this path
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
			Name:   "update",
			Usage:  "update a doc set",
			Action: update,
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
	fmt.Printf("You may now edit %s\n", f)

}

func build(c *cli.Context) {
	var dashing Dashing

	source_depth := 0
	source := c.String("source")
	if len(source) == 0 {
		source = "."
	} else {
		source_depth = len(strings.Split(source, "/"))
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
	db, err := initDB(name, true)
	if err != nil {
		fmt.Printf("Failed to create database: %s\n", err)
		return
	}
	defer db.Close()
	texasRanger(source, source_depth, name, dashing, db)
}

func update(c *cli.Context) {
	var dashing Dashing

	source_depth := 0
	source := c.String("source")
	if len(source) == 0 {
		source = "."
	} else {
		source_depth = len(strings.Split(source, "/"))
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
	db, err := initDB(name, false)
	if err != nil {
		fmt.Printf("Failed to create database: %s\n", err)
		return
	}
	defer db.Close()
	texasRanger(source, source_depth, name, dashing, db)
}

func decodeSingleTransform(val map[string]interface{}) (*Transform, error) {
	var ttype, trep, attr string
	var creg, cmatchpath, requireText *regexp.Regexp
	var err error

	if r, ok := val["attr"]; ok {
		attr = r.(string)
	}

	if r, ok := val["type"]; ok {
		ttype = r.(string)
	}
	if r, ok := val["regexp"]; ok {
		creg, err = regexp.Compile(r.(string))
		if err != nil {
			return nil, fmt.Errorf("failed to compile regexp '%s': %s", r.(string), err)
		}
	}
	if r, ok := val["replacement"]; ok {
		trep = r.(string)
	}
	if r, ok := val["requiretext"]; ok {
		requireText, err = regexp.Compile(r.(string))
		if err != nil {
			return nil, fmt.Errorf("failed to compile regexp '%s': %s", r.(string), err)
		}
	}
	if r, ok := val["matchpath"]; ok {
		cmatchpath, err = regexp.Compile(r.(string))
		if err != nil {
			return nil, fmt.Errorf("failed to compile regexp '%s': %s", r.(string), err)
		}
	}
	return &Transform{
		Type:        ttype,
		Attribute:   attr,
		Regexp:      creg,
		Replacement: trep,
		RequireText: requireText,
		MatchPath:   cmatchpath,
	}, nil
}

func decodeSelectField(d *Dashing) error {
	d.selectors = make(map[string][]*Transform, len(d.Selectors))
	for sel, val := range d.Selectors {
		var trans *Transform
		var err error
		rv := reflect.Indirect(reflect.ValueOf(val))
		if rv.Kind() == reflect.String {
			trans = &Transform{
				Type: val.(string),
			}
			d.selectors[sel] = append(d.selectors[sel], trans)
		} else if rv.Kind() == reflect.Map {
			val := val.(map[string]interface{})
			if trans, err = decodeSingleTransform(val); err != nil {
				return err
			}
			d.selectors[sel] = append(d.selectors[sel], trans)
		} else if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				element := rv.Index(i).Interface().(map[string]interface{})
				if trans, err = decodeSingleTransform(element); err != nil {
					return err
				}
				d.selectors[sel] = append(d.selectors[sel], trans)
			}
		} else {
			return fmt.Errorf("Expected string or map. Kind is %s.", rv.Kind().String())
		}
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
		"Name":        name,
		"FancyName":   fancyName,
		"Index":       config.Index,
		"AllowJS":     aj,
		"ExternalURL": config.ExternalURL,
	}

	err := t.Execute(&file, tvars)
	if err != nil {
		fmt.Printf("Failed: %s\n", err)
		return
	}
	ioutil.WriteFile(name+".docset/Contents/Info.plist", file.Bytes(), 0755)
}

func initDB(name string, fresh bool) (*sql.DB, error) {
	dbname := name + ".docset/Contents/Resources/docSet.dsidx"

	if fresh {
		os.Remove(dbname)
	}

	db, err := sql.Open("sqlite3", dbname)
	if err != nil {
		return db, err
	}

	if fresh {
		if _, err := db.Exec(`CREATE TABLE searchIndex(id INTEGER PRIMARY KEY, name TEXT, type TEXT, path TEXT)`); err != nil {
			return db, err
		}
		if _, err := db.Exec(`CREATE UNIQUE INDEX anchor ON searchIndex (name, type, path)`); err != nil {
			return db, err
		}
	}

	return db, nil
}

// texasRanger is... wait for it... a WALKER!
func texasRanger(base string, base_depth int, name string, dashing Dashing, db *sql.DB) error {
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
			found, err := parseHTML(path, base_depth, dest, dashing)
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

func encodeHTMLentities(orig string) string {
	escaped := new(bytes.Buffer)
	for _, c := range orig {
		if point_to_entity[c] == "" {
			escaped.WriteRune(c)
		} else {
			escaped.WriteString(point_to_entity[c])
		}
	}

	return escaped.String()
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

	content_bytes := new(bytes.Buffer)
	html.Render(content_bytes, root)
	content := encodeHTMLentities(content_bytes.String())

	_, err = out.WriteString(content)
	return err
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

func parseHTML(path string, source_depth int, dest string, dashing Dashing) ([]*reference, error) {
	refs := []*reference{}

	r, err := os.Open(path)
	if err != nil {
		return refs, err
	}
	defer r.Close()
	top, err := html.Parse(r)

	root := css.MustCompile("*[href],*[src]")
	roots := root.MatchAll(top)
	for _, node := range roots {
		for i, attribute := range node.Attr {
			if "href" == attribute.Key || "src" == attribute.Key {
				if strings.HasPrefix(attribute.Val, "/") {
					// parts of the path - the file name - the source depth
					path_depth := len(strings.Split(attribute.Val[1:], "/")) - 1 - source_depth
					relative := ""
					if path_depth > 0 {
						strings.Repeat("../", path_depth)
					}
					node.Attr[i].Val = relative + attribute.Val[1:]
				}
				break
			}
		}
	}

	for pattern, sels := range dashing.selectors {
		for _, sel := range sels {
			// Skip this selector if file path doesn't match
			if sel.MatchPath != nil && !sel.MatchPath.MatchString(path) {
				continue
			}

			m := css.MustCompile(pattern)
			found := m.MatchAll(top)
			for _, n := range found {
				textString := text(n)
				if sel.RequireText != nil && !sel.RequireText.MatchString(textString) {
					fmt.Printf("Skipping entry for '%s' (Text not matching given regexp '%v')\n", textString, sel.RequireText)
					continue
				}
				var name string
				if len(sel.Attribute) != 0 {
					name = attr(n, sel.Attribute)
				} else {
					name = textString
				}

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

func attr(node *html.Node, key string) string {
	for _, a := range node.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
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

var point_to_entity = map[rune]string{
	8704: "&forall;",
	8194: "&ensp;",
	8195: "&emsp;",
	8709: "&empty;",
	8711: "&nabla;",
	8712: "&isin;",
	8201: "&thinsp;",
	8715: "&ni;",
	8204: "&zwnj;",
	8205: "&zwj;",
	8206: "&lrm;",
	8719: "&prod;",
	8721: "&sum;",
	8722: "&minus;",
	8211: "&ndash;",
	8212: "&mdash;",
	8727: "&lowast;",
	8216: "&lsquo;",
	8217: "&rsquo;",
	8730: "&radic;",
	175:  "&macr;",
	8220: "&ldquo;",
	8221: "&rdquo;",
	8222: "&bdquo;",
	8224: "&dagger;",
	8225: "&Dagger;",
	8226: "&bull;",
	8230: "&hellip;",
	8743: "&and;",
	8744: "&or;",
	8745: "&cap;",
	8746: "&cup;",
	8747: "&int;",
	8240: "&permil;",
	8242: "&prime;",
	8243: "&Prime;",
	8756: "&there4;",
	8713: "&notin;",
	8249: "&lsaquo;",
	8250: "&rsaquo;",
	8764: "&sim;",
	// 62:   "&gt;",	// this is already encoded for us
	8629: "&crarr;",
	9824: "&spades;",
	8260: "&frasl;",
	8773: "&cong;",
	8776: "&asymp;",
	8207: "&rlm;",
	9829: "&hearts;",
	8800: "&ne;",
	8801: "&equiv;",
	9827: "&clubs;",
	8804: "&le;",
	8805: "&ge;",
	9830: "&diams;",
	// 38:   "&amp;",	// this is already encoded for us
	8834: "&sub;",
	8835: "&sup;",
	8836: "&nsub;",
	8838: "&sube;",
	8839: "&supe;",
	8853: "&oplus;",
	8855: "&otimes;",
	8734: "&infin;",
	8218: "&sbquo;",
	8901: "&sdot;",
	160:  "&nbsp;",
	161:  "&iexcl;",
	162:  "&cent;",
	163:  "&pound;",
	164:  "&curren;",
	8869: "&perp;",
	166:  "&brvbar;",
	167:  "&sect;",
	168:  "&uml;",
	169:  "&copy;",
	170:  "&ordf;",
	171:  "&laquo;",
	8364: "&euro;",
	173:  "&shy;",
	174:  "&reg;",
	8733: "&prop;",
	176:  "&deg;",
	177:  "&plusmn;",
	178:  "&sup2;",
	179:  "&sup3;",
	180:  "&acute;",
	181:  "&micro;",
	182:  "&para;",
	183:  "&middot;",
	184:  "&cedil;",
	185:  "&sup1;",
	186:  "&ordm;",
	187:  "&raquo;",
	188:  "&frac14;",
	189:  "&frac12;",
	190:  "&frac34;",
	191:  "&iquest;",
	192:  "&Agrave;",
	193:  "&Aacute;",
	194:  "&Acirc;",
	195:  "&Atilde;",
	196:  "&Auml;",
	197:  "&Aring;",
	198:  "&AElig;",
	199:  "&Ccedil;",
	200:  "&Egrave;",
	201:  "&Eacute;",
	202:  "&Ecirc;",
	203:  "&Euml;",
	204:  "&Igrave;",
	// 34:   "&quot;",	// this is already encoded
	206:  "&Icirc;",
	207:  "&Iuml;",
	208:  "&ETH;",
	209:  "&Ntilde;",
	210:  "&Ograve;",
	211:  "&Oacute;",
	212:  "&Ocirc;",
	213:  "&Otilde;",
	214:  "&Ouml;",
	215:  "&times;",
	216:  "&Oslash;",
	217:  "&Ugrave;",
	218:  "&Uacute;",
	219:  "&Ucirc;",
	220:  "&Uuml;",
	221:  "&Yacute;",
	222:  "&THORN;",
	223:  "&szlig;",
	224:  "&agrave;",
	225:  "&aacute;",
	226:  "&acirc;",
	227:  "&atilde;",
	228:  "&auml;",
	229:  "&aring;",
	230:  "&aelig;",
	231:  "&ccedil;",
	232:  "&egrave;",
	205:  "&Iacute;",
	234:  "&ecirc;",
	235:  "&euml;",
	236:  "&igrave;",
	8658: "&rArr;",
	238:  "&icirc;",
	239:  "&iuml;",
	240:  "&eth;",
	241:  "&ntilde;",
	242:  "&ograve;",
	243:  "&oacute;",
	244:  "&ocirc;",
	245:  "&otilde;",
	246:  "&ouml;",
	247:  "&divide;",
	248:  "&oslash;",
	249:  "&ugrave;",
	250:  "&uacute;",
	251:  "&ucirc;",
	252:  "&uuml;",
	253:  "&yacute;",
	254:  "&thorn;",
	255:  "&yuml;",
	172:  "&not;",
	8968: "&lceil;",
	8969: "&rceil;",
	8970: "&lfloor;",
	8971: "&rfloor;",
	8465: "&image;",
	8472: "&weierp;",
	8476: "&real;",
	8482: "&trade;",
	732:  "&tilde;",
	9002: "&rang;",
	8736: "&ang;",
	402:  "&fnof;",
	8706: "&part;",
	8501: "&alefsym;",
	710:  "&circ;",
	338:  "&OElig;",
	339:  "&oelig;",
	352:  "&Scaron;",
	353:  "&scaron;",
	8593: "&uarr;",
	// 60:   "&lt;",	// this is already encoded for us
	8594: "&rarr;",
	8707: "&exist;",
	8595: "&darr;",
	8254: "&oline;",
	233:  "&eacute;",
	376:  "&Yuml;",
	916:  "&Delta;",
	237:  "&iacute;",
	8592: "&larr;",
	913:  "&Alpha;",
	914:  "&Beta;",
	915:  "&Gamma;",
	8596: "&harr;",
	917:  "&Epsilon;",
	918:  "&Zeta;",
	919:  "&Eta;",
	920:  "&Theta;",
	921:  "&Iota;",
	922:  "&Kappa;",
	923:  "&Lambda;",
	924:  "&Mu;",
	925:  "&Nu;",
	926:  "&Xi;",
	927:  "&Omicron;",
	928:  "&Pi;",
	929:  "&Rho;",
	931:  "&Sigma;",
	932:  "&Tau;",
	933:  "&Upsilon;",
	934:  "&Phi;",
	935:  "&Chi;",
	936:  "&Psi;",
	937:  "&Omega;",
	945:  "&alpha;",
	946:  "&beta;",
	947:  "&gamma;",
	948:  "&delta;",
	949:  "&epsilon;",
	950:  "&zeta;",
	951:  "&eta;",
	952:  "&theta;",
	953:  "&iota;",
	954:  "&kappa;",
	955:  "&lambda;",
	956:  "&mu;",
	957:  "&nu;",
	958:  "&xi;",
	959:  "&omicron;",
	960:  "&pi;",
	961:  "&rho;",
	962:  "&sigmaf;",
	963:  "&sigma;",
	964:  "&tau;",
	965:  "&upsilon;",
	966:  "&phi;",
	967:  "&chi;",
	968:  "&psi;",
	969:  "&omega;",
	9674: "&loz;",
	8656: "&lArr;",
	977:  "&thetasym;",
	978:  "&upsih;",
	8659: "&dArr;",
	8660: "&hArr;",
	982:  "&piv;",
	165:  "&yen;",
	8657: "&uArr;",
	9001: "&lang;",
}
