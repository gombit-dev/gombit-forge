# Upgrade notes

## Unreleased: portable page filenames (#295)

Page slugs that produce the same filename ignoring case now fail spec validation
with `duplicate_slug`. For example, `open-orders` and `openorders` collide for
two table pages on the same resource, or for two dashboards. Different resource
directories and page-type suffixes remain independent. New pages automatically
receive a numeric suffix when needed; existing slugs and generated filenames are
not rewritten.

**Repair affected projects before upgrading.** An existing colliding head cannot
be built or exported with this validator. The editor cannot rename a slug, and
removing a page or its navigation separately can leave the other validation
error. Even a corrected candidate sent to the revisions endpoint is insufficient
after upgrading: ABI classification also builds the invalid current head.

While running the previous version:

1. Save a copy of the current spec from `GET /api/v1/projects/{projectID}/spec`.
2. In a copy of that spec, change only the later colliding page's `slug` to a
   distinct value, such as `openorders-2`. Keep its ID, resource, type and all
   navigation references unchanged. Check for other collisions, including with
   the replacement slug.
3. Submit the corrected spec through `POST /api/v1/projects/{projectID}/revisions`
   using the normal authenticated session and CSRF header. This creates an
   immutable revision; do not edit stored revision rows.
4. Confirm the corrected head validates and builds with the new version before
   upgrading the deployment. Update bookmarks using the changed page URL.

If already upgraded, use the previous application version to perform the repair
before upgrading again. This change includes no automatic migration or editor
recovery action; older colliding revisions remain unbuildable with the new
validator even after the current head has been repaired.
