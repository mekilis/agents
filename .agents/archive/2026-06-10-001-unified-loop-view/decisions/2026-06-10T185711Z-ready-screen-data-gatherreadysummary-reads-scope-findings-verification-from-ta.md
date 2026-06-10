# Decision: Ready screen data (gatherReadySummary) reads scope/findings/verification from ta

Status: proposed
By: implementer

Ready screen data (gatherReadySummary) reads scope/findings/verification from task chunks and git diff --stat against HEAD, with honest placeholders when git or records are absent; rendering (readySummaryLines) and key handling (runReadyScreen fed by the shared key channel) are pure and unit-tested
