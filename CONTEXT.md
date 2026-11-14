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

**Removal**:
One requested removal of the Albums or Items matched by a query, from its Preview through to its outcome. Confirming deletes their files as well as their records.
_Avoid_: Delete, purge

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
Making an image an Album's art, whether the Album had art or not. It takes effect straight away, so it isn't a Modification and has no Preview; when it replaces art, the UI shows the current and new art side by side first. beets-api archives the previous art.
_Avoid_: Upload, replace cover

**Remove art**:
Clearing an Album's art. beets-api archives the image before deleting its file; the UI asks first.
_Avoid_: Delete art

### Importing

**Import**:
Bringing music files from Import Paths into the Library, equivalent to `beet import`.
_Avoid_: Ingest, upload

**Import Path**:
A directory beets-api allows Imports to read from. Whatever is picked for an Import lies inside one.
_Avoid_: Inbox, source folder, drop folder

**Import Task**:
One Album's or one Item's worth of files within an Import, which needs its own Choice.
_Avoid_: Job

**Candidate**:
A possible metadata match for an Import Task, found by a metadata source.
_Avoid_: Result, suggestion

**Choice**:
What to do with an Import Task, such as applying a Candidate, importing as-is, or skipping.
_Avoid_: Decision, answer

### Audit

**Audit**:
The append-only record beets-api keeps of every change it made to the Library. Previews and rejected Modifications change nothing, so they aren't in it.
_Avoid_: History, log

**Audit entry**:
One change to one Album or Item (including its art): when, by which Client and Actor, what changed, and the Modification or Import that caused it.
_Avoid_: Event, revision

**Client**:
The program calling beets-api, as it names itself; this UI is `beets-ui`. A label, not proof.
_Avoid_: User, caller

**Actor**:
The person using the UI, as the proxy in front of it says (from the `Remote-User` header by default). beets-ui passes it to beets-api, which records it on each Audit entry and each Choice of an Import. Not proof either: it holds only while the proxy is the one way in.
_Avoid_: User, account, Client
