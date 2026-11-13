Right below the each mosaic grid cell draw a guest's name (or identity)
using centered vertical alignment. Provide some additional vertical space for
the label so it would fit the margin b/w cells.

Modify `ci/mosaic/run_guest.sh` so it would accept an optional argument:
`-n|--name 'Vasya Pupkin'`. If the name is not provided it should be
auto-generated somehow or the underlying protocol identity should be used.
