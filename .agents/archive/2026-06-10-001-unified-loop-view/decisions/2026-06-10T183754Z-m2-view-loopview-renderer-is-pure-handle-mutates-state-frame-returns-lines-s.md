# Decision: M2 view: loopView renderer is pure (handle mutates state, frame returns lines) s

Status: proposed
By: implementer

M2 view: loopView renderer is pure (handle mutates state, frame returns lines) so tests assert frames from a fake event stream; lanes map reviewer right and implementer/coordinator left; output rings cap at 200 lines with ANSI stripped and CR-overwrite collapsed
