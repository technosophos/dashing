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
    "name": "Dashing",    // Name of the package
    "index":"index.html", // The default index in your existing docs.
    "icon32x32": "icon.png", // A 32x32 pixel PNG formatted icon.
    "selectors": {  // CSS selectors (see below)
        "dt a": "Command",
        "title": "Package"
    },
    "ignore": [  // A way to say, "Ignore these things"
        "ABOUT"
    ]
}
```

Dashing uses CSS 3 selectors to map patterns in a document to Dash
sections. You tell Dashing which patterns in HTML map to which Dash data
type. The list of Dash data types can be found here: https://kapeli.com/docsets.

```
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

On occasion, you'll have to manually ignore some matched text bits. To
do that, you can use the `ignores` directive in the JSON file:


```
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
