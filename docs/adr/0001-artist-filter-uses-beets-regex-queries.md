# The Library finds an Artist's Albums and Items with a beets regex query

An Artist is a name split out of `albumartist`/`artist` strings, not a record, so beets has no query for "everything crediting Queen". The UI builds one: a regex query such as `'albumartist::^(?:.*?(?:,|;|&)\s*)?Queen(?:\s*(?:,|;|&).*)?$'`, from the Artist separators that beets-api reports in `GET /config`. We did not ask beets-api for an "Albums by Artist" endpoint, because every list in the UI can start a Modify, and a Modify is defined by a beets query. A list that came from anything other than a query would have no query to Modify "all N matching".

## Consequences

The UI and beets-api must agree on the Artist separators, so beets-api owns them in its config and the UI reads them. Regex queries can't use an index, which is fine at personal-library sizes. Names are escaped for both Python `re` and beets' shell-style query parsing.
