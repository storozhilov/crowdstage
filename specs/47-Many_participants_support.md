Implement the support for many participants in `MosaicComposer`. Introduce a
max participant count limit.

Define the basic amount of rows and columns for the mosaic grid, e.g. 4x3 or
4x4 or whatever.

Center mosaic and stretch it to occupy the entire space of the studio monitor
screen. Keep some space between participant cells and between cells and screen
borders.

When the amount of participans is greater than active mosaic grid cells the
participant video is not streamed to studio monitor screen until the free
mosaic grid cell is to appear (other participant leaves).

`GET /health` REST endpoint should return the state of `MosaicComposer`
('idle'/'running') and the participant count.
