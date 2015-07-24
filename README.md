# Dashing: Generate Dash documentation from HTML

Dashing is a general purpose tool for starting with some HTML
documentation and generating Dash documentation.

Dash can take any HTML documentation and transform it into the internal
Dash format... but first you have to be able to map the HTML to Dash
navigation. This is what Dasher does for you.

https://kapeli.com/docsets

## INSTALL

`go install gitub.com/technosophos/dashing`

## USAGE

To get started, `cd` to the directory that you want to generate
documentation inside.

```
$ cd mydocs
$ dashing create
# Now you can edit dashing.json. See below.
$ dashing build -s path/to/HTML/docs mydocs
```

You will now have a directory called `mydocs.docset` that contains all
the documentation you need for Dash.

## dashing.json Format

Dashing uses CSS 3 selectors to map patterns in a document to Dash
sections. You tell Dashing which patterns in HTML map to which Dash data
type.

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
