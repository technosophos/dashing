# Dashing: Generate Dash documentation from HTML

Dashing is a general purpose tool for starting with some HTML
documentation and generating Dash documentation.

Dashing uses CSS3 selectors to tag an HTML document for import into
Dash. It generates a complete docset for you.

This supports the following Dash features:

- Index pages
- Custom icon
- Table of Contents auto-generation

See:
https://kapeli.com/docsets

## INSTALL

Prebuilt OSX 64-bit binaries are available here:
https://github.com/technosophos/dashing/releases

If you have Go 1.4 or later installed, simply run:

`go get -u github.com/technosophos/dashing`

Dashing will now be located at `$GOPATH/bin/dashing`.

A prebuilt binary is also available as a GitHub release.

## USAGE

To get started, `cd` to the directory that you want to generate
documentation inside.

```
$ cd mydocs
$ dashing create
# Now you can edit dashing.json. See below.
$ dashing build mydocs
```

You will now have a directory called `mydocs.docset` that contains all
the documentation you need for Dash.

For more, run `dashing help`.

## dashing.json Format

The basic Dashing format looks like this:

```json
{
    "name": "Dashing",
    "index":"index.html",
    "icon32x32": "icon.png",
    "selectors": {
        "dt a": "Command",
        "title": "Package"
    },
    "ignore": [
        "ABOUT"
    ]
}
```

- name: Name of the package
- index: Default index file in the existing docs
- icon32x32: a 32x32 pixel PNG icon
- selectors: a map of selectors. There is a simple format and
  a more advanced format (see below for details).
- ignore: a list of matches to be ignored (see below)

Dashing uses CSS 3 selectors to map patterns in a document to Dash
sections. You tell Dashing which patterns in HTML map to which Dash data
type. The list of Dash data types can be found here: https://kapeli.com/docsets.

```json
{
  "selectors": {
    "h1 a": "Package",
    "h2.classdef a": "Class",
  }
}
```

The above will look for `h1 a` combinations, and treat those as package
definitions, and `h2 class="classdef" a` combinations and treat those as
Class definitions.

## Ignoring Sections You Don't Care About

On occasion, you'll have to manually ignore some matched text bits. To
do that, you can use the `ignores` directive in the JSON file:


```json
{
  "selectors": {
    "h1 a": "Package",
    "h2.classdef a": "Class",
  },
  "ignore": ["DESCRIPTION", "MORE"]
}
```

The above will ignore anything whose text matches the exact text "DESCRIPTION"
or "MORE", even if the selectors match.

## Regular Expressions for Substitutions

Instead of using a simple mapping of selector to type, you have the
option of mapping a selector to a substitution pattern.

The format for this type of `selectors` map looks like this:

```json
{
    "name": "BusyBox",
    "package":"busybox",
    "index":"BusyBox.html",
    "icon32x32":"busybox1.png",
    "selectors": {
        "dt a": "Command",
        "title": {
          "type":"Package",
          "regexp": " - The Swiss Army Knife of Embedded Linux",
          "replacement": ""
        }
    },
    "ignore": [
        "ABOUT"
    ]
}
```

The format of the selector value is:

```json
"css selector": {
      "type":"Dash data type",
      "regexp": "PCRE regular expression (no need to enclose in //)",
      "replacement": "Replacement text"
}
```

Full documentation on the regular expression format can be found here:
http://golang.org/pkg/regexp/syntax/

Documentation on the format for `replacement` can be found here:
http://golang.org/pkg/regexp/#Regexp.ReplaceAllString
