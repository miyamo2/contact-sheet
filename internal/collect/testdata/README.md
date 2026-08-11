# Captures

`captures/` is what the collector's tests walk, what CI renders a dry run
against, and what `/dogfooding` attaches to a pull request. That last one is why
the files here are real pictures rather than placeholders: a dogfooding comment
is read as the thing a user would see, and a file that is not a decodable image
shows up in it as a broken image icon. The path resolves, the bytes arrive, and
the comment still looks broken -- so anything added here has to open in a
browser, not merely end in `.png`.

The screenshots are of a site that does not exist. They were taken with headless
chromium of a handful of static pages, at the viewport a run would use:

    desktop-chromium/   1280x800   about, article-list, tags, in light and dark
    mobile-chromium/     390x844   the same three, plus a menu-modal with no
                                   dark counterpart, so a themed template has a
                                   row with one cell filled and one empty
    flat/               1280x800   two screens in one directory, for a layout
                                   with nothing to group by

`captures/desktop-chromium/trace.zip` is deliberately not an image, and its
contents are deliberately not a zip: it stands for the playwright trace that
sits next to a run's screenshots, and the collector has to leave it alone
whether or not a layout expression is filtering. The tests assert 13 images
under `captures/`, so adding or removing one there means updating them.
