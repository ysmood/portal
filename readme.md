
# Build

For linux x64

```bash
GOOS=linux GOARCH=amd64 go build
```

# Protocol

Portal is a high performance frontend service. It depends on a backend to
serve the files.

When an end user want to read a file from Portal, Portal will try
to send a http request to `FileService` to get `rawFile`, then
compute the `rawFile` and return the final `file` to end user.

```
             rawFile             file
FileService ----------> Portal --------> EndUser
```

`Portal` requests a file's `uri` to `FileService`. `uri` is the file location in the `FileService`.

```bash
curl -v http://127.0.0.1:7000/api/file?uri={uri}
```

The `rawFile` type can be `binary` or `gisp`.

### When `rawFile` is `binary`

Portal returns the `rawFile` as `file` directly.
The response format will be the same as normal static http server.

#### When `rawFile` is `gisp`

Portal will execute the script then return the result as `file`.

The response format will be like:

```
HTTP/1.1 200 OK
Portm-Type: Gisp

{gisp code}
```

```
HTTP/1.1 200 OK
Portm-Type: Binary

{bin}
```



# Dev

```bash
noe -b go -w 'lib/**/*.go' -w '*.go' -- get
```

# Changelog

## v1.2

- Remove query whitelist. All queries not begin with `portal-` will be ignored.

