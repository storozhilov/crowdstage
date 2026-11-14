Rearrange `MixerConsole` interface.

GUI should occupy all the horizontal space of the app web-page.

Place **Control** panel to the top and make it to look like header-less stripe
with the two equal parts: one is for `MixerConsole` on the left and another is
for `MosaicComposer` on the right. Each part should display in a line:

- The on/off switch on the left, which is followed by ..
- .. the main parameters, like LiveKit connection status and
  guests/inputs/outputs count.

When to click on **Control** panel it should slide down and display all the
tracked parameters, say a "full view". If to click it again somewhere it should
slide up to the initial position/rendering.

Consider the main area below the **Control** panel as a 3x3 grid with cells of
the same hight and the same width.

**Broadcast** panel should occupy left-top 2x2 subgrid cells with coordinates:
[1,1], [1,2], [2,1] and [2,2].

**Guests** panel should occupy cells with coordinates [1,3] and [2,3]. Make
mosaic video player to gravitate to the top side of it.

**Studio** panel should occupy the bottom-line cells: [3,1], [3,2] and [3,3].
Make studio video player to gravitate to the left side of it.
