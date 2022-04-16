# rendmail

[![Build Status](https://storage.googleapis.com/derat-build-badges/d239ac0b-744c-4c4e-a788-e3aea4a855ba.svg)](https://storage.googleapis.com/derat-build-badges/d239ac0b-744c-4c4e-a788-e3aea4a855ba.html)

> **[rend]** _(transitive verb)_: To separate into parts with force or sudden
> violence; to split; to burst

rendmail is a command-line program that reads an email message from stdin and
writes it (possibly with modifications) to stdout. It can be executed by a
[message delivery agent] like [procmail] or [fdm] to remove binary attachments
from messages before they are delivered.

rendmail aims to be fast and minimally intrusive (contrary to its name).
Messages are read into memory one line at a time and are not written to disk,
and the original bytes are unmodified to the full extent possible.

[rend]: https://en.wiktionary.org/wiki/rend
[message delivery agent]: https://en.wikipedia.org/wiki/Message_delivery_agent
[procmail]: https://en.wikipedia.org/wiki/Procmail
[fdm]: https://github.com/nicm/fdm

## Background

I periodically download messages from a webmail service for local backup.
However, I don't want to include bulky images or other binary attachments in
backups; using the webmail service to preserve those is good enough for me. I
used to manually delete these attachments using [mutt], but I found doing so to
be tedious. I couldn't find much in the way of software for deleting attachments
from messages, so I decided to write my own.

[mutt]: http://www.mutt.org/

## Usage

rendmail can be compiled and installed by first [installing Go] and then
running `go install` from the root of this repository.

The `rendmail` executable accepts various command-line flags:

```
Usage: rendmail [flag]...
Reads an email message from stdin and rewrites it to stdout.

  -backup-dir string
        Directory to which original, unmodified message will be saved
  -delete-binary
        Delete common binary attachments from message
  -delete-types string
        Comma-separated globs of attachment media types to delete
  -fake-now string
        Hardcoded RFC 3339 time (only used for testing)
  -keep-types string
        Comma-separated glob overrides for -delete-types
  -verbose
        Write informative messages to stderr
```

**Rewriting email is scary.** You may want to initially pass `-backup-dir
/some/path` to rendmail to save unmodified copies of messages to a temporary
location in case something goes wrong.

Please file an issue if you encounter messages that rendmail has trouble
processing.

[installing Go]: https://go.dev/doc/install

### procmail

For [procmail], a recipe like the following can be added near the top of
[.procmailrc] to pipe all messages through rendmail to remove binary attachments
before other recipes are evaluated:

```
# h: Process message header
# b: Process message body
# f: Recipe is a filter (messages remain in input stream)
# w: Wait for program to complete and check its exit code
:0 hbfw
| rendmail -delete-binary
```

If the `rendmail` executable isn't already in procmail's default `PATH`, you'll
probably need to supply an absolute path or set `PATH` within `.procmailrc`.

[.procmailrc]: https://manpages.debian.org/stable/procmail/procmailrc.5.en.html

### fdm

For [fdm], the following rule can be added near the top of [.fdm.conf] to pipe
all messages through rendmail to remove binary attachments:

```
match all
      action rewrite "rendmail -delete-binary"
      continue
```

[.fdm.conf]: https://manpages.debian.org/stable/fdm/fdm.conf.5.en.html

## Further reading

The following RFCs are relevant to rendmail:

*   [RFC 5322]: Internet Message Format
    (supersedes [RFC 2822] and [RFC 822])
*   [RFC 2045]: MIME Part One: Format of Internet Message Bodies
    (supersedes [RFC 1521] and [RFC 1341])
*   [RFC 2046]: MIME Part Two: Media Types
*   [RFC 2047]: MIME Part Three: Message Header Extensions for Non-ASCII Text

[RFC 5322]: https://www.rfc-editor.org/rfc/rfc5322
[RFC 2822]: https://www.rfc-editor.org/rfc/rfc2822
[RFC 822]: https://www.rfc-editor.org/rfc/rfc822
[RFC 2045]: https://www.rfc-editor.org/rfc/rfc2045
[RFC 1521]: https://www.rfc-editor.org/rfc/rfc1521
[RFC 1341]: https://www.rfc-editor.org/rfc/rfc1341
[RFC 2046]: https://www.rfc-editor.org/rfc/rfc2046
[RFC 2047]: https://www.rfc-editor.org/rfc/rfc2047

I would've liked to use Go's [net/mail] and [mime/multipart] packages to do all
of the hard work and call it a day, but doing so would've required messages to
be completely rewritten (i.e. with headers reordered and bodies reencoded).
[This Go issue](https://github.com/golang/go/issues/50868) requested making the
[net/textproto] package's `ReadMIMEHeader` function preserve header field order,
but it was closed.

[net/mail]: https://pkg.go.dev/net/mail
[mime/multipart]: https://pkg.go.dev/mime/multipart
[net/textproto]: https://pkg.go.dev/net/textproto

The `copy_delete_attach()` function in [mutt's copy.c file] looks like it's
responsible for deleting attachments from multipart messages. rendmail follows
the approach used there:

*   Replace the original `Content-Type` header field with one with media type
    `message/external-body`.
*   Write a blank line to terminate the part's header.
*   Write the remaining header fields as the part's body.

[mutt's copy.c file]: https://github.com/muttmua/mutt/blob/master/copy.c

Here are some related programs that I came across:

*   [MIMEDefang] uses the [milter] protocol to filter or modify email on behalf
    of the [Sendmail] MTA. It's written in C and Perl.
*   [Mailmunge] is the successor to MIMEDefang, and also uses milter to interact
    with Sendmail and is written in C and Perl.
*   [Email Sanitizer] was designed to integrate with procmail and appears to
    support removing attachments from messages based on MIME type (along with
    many other operations). I think it's a mix of procmail rules and Perl, and
    it looks like development stopped around 2006.
*   [Stripmime] is a small Perl script that strips HTML and binary parts from
    messages that are sent to mailing lists. I think development stopped around
    2005.
*   [Demime] looks like it was another Perl script for convert messages into
    plain text for mailing lists. It's unsupported, as far as I can tell (its
    webpage no longer exists).
*   [alterMIME] is a C program "used to alter your mime-encoded mailpacks as
    typically received by Inflex, Xamime and AMaViS". I have to admit that I
    still don't know what a mailpack is after multiple web searches. It looks
    like the last release was in 2008.

[MIMEDefang]: https://mimedefang.org/
[milter]: https://en.wikipedia.org/wiki/Milter
[Sendmail]: https://en.wikipedia.org/wiki/Sendmail
[Mailmunge]: https://www.mailmunge.org/
[Email Sanitizer]: https://www.mailmunge.org/
[Stripmime]: https://www.phred.org/~alex/stripmime.html
[Demime]: http://web.archive.org/web/20070814043830/http://scifi.squawk.com/demime.html
[alterMIME]: https://pldaniels.com/altermime/

I found Enrico Zini's [Migrating from procmail to sieve] post to be interesting.
It led me to Anarcat's [procmail considered harmful] page, which pointed me to
Nathan Willis's [Reports of procmail's death are not terribly exaggerated] LWN
article from 2010.

[Migrating from procmail to sieve]: https://www.enricozini.org/blog/2022/debian/migrating-from-procmail-to-sieve/
[procmail considered harmful]: https://anarc.at/blog/2022-03-02-procmail-considered-harmful/
[Reports of procmail's death are not terribly exaggerated]: https://lwn.net/Articles/416901/
