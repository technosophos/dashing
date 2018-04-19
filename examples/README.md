# Examples

You can start with these simple examples and go from there.

## BusyBox

BusyBox is an embedded linux tool that provides many frequently used
shell commands.

This example is fairly complete, including an icon.

```
$ cd busybox
$ wget http://www.busybox.net/downloads/BusyBox.html
$ dashing build
```

## Dockerfile

This example builds documentation for Docker Dockerfiles. The CSS
selectors are just slightly more complicated, but nothing to cause
heartburn.

This example illustrates the usage of `ignore` fields.

```
$ cd dockderfile
$ wget https://docs.docker.com/engine/reference/builder/
$ dashing build
```

## Kubernetes kubectl

This example shows you had to fetch a remote set of documents and then
build locally with dashing.

If you don't have it already, you will need `wget`:

```
$ brew install wget
```

Now we can fetch the documentation:

```
$ wget -k -r -p -np http://kubernetes.io/v1.0/docs/user-guide/kubectl/kubectl.html
```

This will leave you with a directory called `kubernetes.io` that
contains all of the Kubectl documentation, along with relevant JS, CSS,
and images.


