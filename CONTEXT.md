# Beets UI

A web UI for browsing a beets library and changing its metadata, through beets-api. It uses beets-api's language (and so beets' own) and adds no synonyms of its own.

## Language

### Library

**Library**:
The beets database of music, together with the directory holding its files.
_Avoid_: Collection, catalog

**Item**:
A single music file known to the Library.
_Avoid_: Track, song

**Album**:
A group of Items stored as one record in the Library, whose album-level fields can be copied down to its Items.
_Avoid_: Release, record

**Artist**:
One name found by splitting Albums' `albumartist` and Items' `artist` on the artist separators. Not a record in the Library: it exists only while some Album or Item credits it. One Album or Item can credit several Artists ("Jay-Z & Beyoncé" credits Jay-Z and Beyoncé), and an Item on an Album can credit an Artist the Album doesn't. A name is matched whole, so "Prince" doesn't match "Princess Nokia".
_Avoid_: Performer, band

**Artist separators**:
The strings that divide one credit into several Artists, `,` `;` and `&` by default. Words such as "and" or "feat." are not separators unless configured.

**Album art**:
The image file an Album points to with `artpath`, kept in the Album's directory.
_Avoid_: Cover, artwork

### Changing metadata

**Modify**:
A change to the metadata of the Items or Albums matched by a query, equivalent to `beet modify`. Changing a single record is a Modify whose query matches only that record.
_Avoid_: Edit, batch edit

**Modification**:
One requested Modify, from its Preview through to its outcome.
_Avoid_: Edit, job

**Scope**:
Whether a query (in a Modify or a list) matches Items or Albums, like `beet modify` with or without `-a`. One Modification has one Scope; Albums and Items are never mixed.
_Avoid_: Kind, album flag, mode

**Preview**:
The exact changes a Modification would make, shown before anything changes. The UI always shows it before a Confirm.
_Avoid_: Diff, pretend, dry run

**Confirm**:
Approving a whole Preview so its changes get applied.
_Avoid_: Approve, commit

**Set art**:
Making an image an Album's art, whether the Album had art or not. It replaces and deletes any previous art file straight away, so it isn't a Modification and has no Preview; when it replaces art, the UI shows the current and new art side by side first.
_Avoid_: Upload, replace cover
