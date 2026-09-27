# Emoji table

`emoji.tsv` is the launcher's emoji table: one emoji per line,
`<emoji>\t<name>\t<keyword>|<keyword>|...`, in `emoji-test.txt` order. It is
generated at authoring time by `tools/emojigen` and committed, so the build
stays offline and reproducible.

## Upstream

| Source | URL | SHA-256 |
|---|---|---|
| Unicode 16.0 emoji test data | `https://www.unicode.org/Public/emoji/16.0/emoji-test.txt` | `24f0c534e86cf142e2496953e8f0e46a3e702392911eddcd29c6cced85139697` |
| CLDR 46 English annotations | `https://raw.githubusercontent.com/unicode-org/cldr/release-46/common/annotations/en.xml` | `7d491f1782480d50ee953d3943e8a65eacaa4394b41af9e9f89c9be418256ade` |
| CLDR 46 English derived annotations | `https://raw.githubusercontent.com/unicode-org/cldr/release-46/common/annotationsDerived/en.xml` | `461d1578079c5ebc947e506df6b5a55c93f006160e8dff3f05dcf917ce081604` |

Licence: Unicode License v3, copied verbatim to `LICENSE`
(`https://www.unicode.org/license.txt`).

## Selection

- `fully-qualified` sequences only; the `Component` group and every sequence
  carrying a skin-tone modifier (U+1F3FB–U+1F3FF) are skipped.
- The name is the `emoji-test.txt` comment after its `E<version>` token.
- Keywords are the CLDR non-`tts` annotation for the sequence, base file first,
  then derived; a sequence CLDR does not list is looked up again without its
  U+FE0F variation selectors. The name itself is dropped from the keywords.

Result: **1,906 rows**, generated 2026-09-28.

## Reproducing

```bash
GOWORK=off go run ./tools/emojigen -out internal/emoji/emoji.tsv \
  -sha-test 24f0c534e86cf142e2496953e8f0e46a3e702392911eddcd29c6cced85139697 \
  -sha-ann 7d491f1782480d50ee953d3943e8a65eacaa4394b41af9e9f89c9be418256ade \
  -sha-derived 461d1578079c5ebc947e506df6b5a55c93f006160e8dff3f05dcf917ce081604
git diff --exit-code -- internal/emoji/emoji.tsv
```

The generator refuses a download whose SHA-256 differs. The committed table's
own SHA-256:

```
0a228abdeca94742074b593282423468833ba6a3fc21015b4db71160b0e92796
```
