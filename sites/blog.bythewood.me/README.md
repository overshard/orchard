# blog.bythewood.me

A Markdown blog with no database. Posts are files in `content/posts`, parsed
once at startup and served from memory, and each one also gets a PDF and a
social card built by Typst, plus a Markdown copy, search, tag and year pages,
and an Atom feed.

## Requirements

Go and bun, and typst on the PATH for the PDFs and the cards. No `.env`.

## Running it

```sh
make run      vite watch + go run, on :8000
make pdfs     every post's PDF into build/pdfs
make og       every social card into build/og
make build    all of the above, then the release binary into ../../bin/blog.bythewood.me
make clean    delete build/ and node_modules
```

A new post is a Markdown file with `title`, `slug`, `date`, `publish_date`,
`tags`, `description` and `cover_image` in its front matter, and its images go
in `content/images`.

## Layout

`posts.go` loads the posts, `markdown.go` renders them with goldmark and chroma,
and `typst.go` with `pdf.go` and `og.go` writes the PDFs and cards. `feed.go`,
`search.go`, `seo.go` and `jsonld.go` are what their names say. A release build
embeds the posts, the bundle, the PDFs and the cards, so the binary is the
whole blog and the image ends at `FROM scratch`.

## Things that will bite you

- Publishing is a deploy, since posts load once at startup. A future
  `publish_date` doesn't need one though, the post appears on its own that day.
- Without typst a local checkout answers 404 for every PDF.
- The cards and PDFs need Geist's TrueType files, which only exist after
  `make frontend` has installed the bun package.
