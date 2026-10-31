# Web interface audit (issue #78)

Walked through with the real program on a throw-away settings file, in Microsoft Edge (headed), at 1366x768 and 390x844, light and dark, with fake PCs made through the
program's own protocols: DHCP and TFTP packets (a PC that only asked for a boot file; one that took the whole Secure Boot shim and never started iPXE), `/menu` requests (a normal
UEFI PC, a BIOS PC, a PC with very long maker, model and name, a PC with an offered host name and asset tag) and the API (names, a rule, kernel options). Pages looked at: Dashboard,
Server, Library, ISOs, Unattended, PCs, Menu, Log, Account, the first-start guide and the login. The interface has one language (English) and no right-to-left layout.

Screenshots (fake data only) are in [audit/](audit/): `pcs-before-squeezed-select.png` and `pcs-dark-before.png` are the PCs page before this audit's fixes, `pcs-after-readable-select.png`
and `pcs-dark-after.png` after.

## Fixed on branch `improve/web-ui-1`

| Severity | Finding | Fix |
|---|---|---|
| High | PCs page: the drop-down that sets what a PC does at start was squeezed to 2-3 letters ("Boc", "Shc") by the row of six buttons that could not wrap, so no rule could be read at a glance (1366 wide). | The select keeps 14 characters; the buttons wrap. |
| Medium | Dashboard and Server pages scrolled sideways on a phone (the library path, the settings file path, a `nowrap` label column). | Long values wrap on a phone. |
| Medium | Dark theme: red text (links, current page, red badges, error banners) measured 3.45:1 on the panels, below AA (4.5:1). | A separate lighter red for text (5.9:1); buttons and switches keep Gola Red. |
| Medium | Outline of text boxes and drop-downs and the "off" track of a switch were 1.4:1 against the page in both themes (WCAG 1.4.11 asks 3:1). | New `--field` colour, 3.2:1 or more. |
| Low | An address ("127.0.0.1") and "4 min ago" broke in the middle on the PCs page. | Kept on one line. |
| Low (#70) | Dialogs let Tab reach the page behind. | Tab is trapped inside the dialog. |

## Open findings (not changed: they need a design decision)

1. **PCs page: the rule drop-down mixes two kinds of entries** (plain ISO start through iPXE, and "Install ..., once (works with Secure Boot)"), and says nothing when a plain rule is set
   while the Secure Boot route is on. Proposal: group the entries in `<optgroup>` by what they need, mark the Secure Boot ones, show a one-line warning on the row.
2. **PCs page: repeated and long text per row.** Each "asked for a boot file" row has a four-line hint plus a "Doing" column that repeats the route ("secure boot: shim, then signed iPXE (built in)"),
   the same for every row. Proposal: one short status badge per row ("Asked for a boot file", "Took the shim, never started iPXE", "Windows offer"), the explanation behind a "Why?"
   disclosure, and the route line only in History.
3. **PCs page: six buttons per row** (Name, Kernel options, History, Wake, Remove rule, Forget). Proposal: keep Name and History visible, move Kernel options, Wake and Forget into a
   "More" menu, and keep "Remove rule" beside the rule drop-down.
4. **Column "Last seen" mixes two facts** (time and "1x menu"). Proposal: the menu count goes into History.
5. **Navigation by address.** `#pcs` and the other addresses are read only when the page loads: there is no `hashchange` handler, so the browser's Back button leaves the interface and a link
   inside the page does not move to a page. Proposal: listen to `hashchange` and use `history.pushState` for the page buttons.
6. **First-start guide.** While it is shown no item of the menu is marked as the current page, and a `#pcs` address is ignored until the guide is finished or skipped. Proposal: show it as a banner
   above the Dashboard instead of replacing the page.
7. **Phone width.** The page list scrolls sideways with no sign that "Menu", "Log" and "Account" exist; the PCs page is about 5500 px tall because every row is a card. Proposal: a fade or arrow at
   the edge of the list; compact rows with the rule drop-down and one "More" button.
8. **Switches and selects have no visible state text**; the on/off switch relies on colour only (red when on). Proposal: a text label ("In the menu" / "Hidden") beside it.
9. **Rule drop-down on narrow desktop windows** (about 900 px) takes 14 characters and the buttons wrap onto two lines; with finding 3 this becomes one line.
10. **Windows set-up drivers**: the tick and the folder are in the Tuning block of the Server page, far from the Windows rule (from the issue). Proposal: show the state next to a Windows rule and
    link to the setting.
11. **Server page, Secure Boot block, locked settings** greyed without saying which option or file locks them (from the issue).
12. **Screenshots not covered**: the ISOs and Library pages were seen only empty (no ISO in the throw-away library), and the Windows-native offer ("offered", "choose") could not be reproduced without a
    Windows ISO, so only the "none" state (no suitable ISO) was seen.

## Tooltips

The interface has no custom tooltip: every hint is a `title` attribute (the debug switches, the Secure Boot button, the "as a disk" badge, the SHA-256 and the source of a file, the theme
button), which the browser places and keeps inside the screen. Nothing needs a clamp today. Limits to keep in mind: a `title` is not shown on a phone or by keyboard, so no instruction may
exist only there (the debug switches' explanations do also appear in the documentation, but not on the page); a future custom tooltip must be positioned with an explicit clamp to the
viewport (`left`/`top` limited to `[8, innerWidth - width - 8]`), use logical properties (`inset-inline-start`) so that it also works right to left, and be reachable by keyboard focus.
Proposal: replace the three long `title` explanations by a visible one-line hint under the switches.

## Contrast and states (measured)

| Pair | Light | Dark |
|---|---|---|
| Muted text on panel | 6.2 | 6.7 |
| Gola Red text on panel (after) | 6.4 | 5.9 |
| White on the red button | 6.4 | 4.7 |
| Green, amber, red badges on their tints | 4.8 / 5.4 / 5.5 | 6.8 / 7.8 / 6.0 |
| Focus ring on panel | 5.9 | 6.8 |
| Form outline on panel (after) | 3.7 | 4.8 |

Focus rings are a 3 px blue outline on every control; the one accent colour is Gola Red, plus green and amber for state. Keyboard order was checked only in the dialogs (Tab and
Shift+Tab, Escape, focus returned).

## Not validated

Right-to-left layouts and other languages (the interface has none); real devices and other browsers (only Edge); screen readers; the boot-entries dialog in a browser (no ISO with several
entries); the Windows offer states "offered" and "choose".
