Implement a multi-guest support in `MosaicComposer`.  Let's consider
system should support 4 guests max.  So the mosaic could be like a 2x2
grid.

Implement an e2e test which should imitate a situation where guests join
and leave the event a kinda randomly so the total amount of guests vary
from 0 to 4 during the test.
