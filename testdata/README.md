# testdata

This directory contains email messages used to test the `rewriteMessage`
function.

Files with a `.in.txt` suffix are used as input, while corresponding `.out.txt`
files contain expected output.

File with an `sa_` prefix were downloaded from the [SpamAssassin corpus] on
2022-04-13. Leading non-header `From` envelope lines were manually deleted when
present. As stated in [the original readme file],

> Copyright for the text in the messages remains with the original senders.

[SpamAssassin corpus]: https://spamassassin.apache.org/old/publiccorpus/
[the original readme file]: https://spamassassin.apache.org/old/publiccorpus/readme.html
