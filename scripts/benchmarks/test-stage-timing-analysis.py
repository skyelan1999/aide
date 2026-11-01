import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("stage_timing", Path(__file__).with_name("analyze-stage-timing.py"))
stage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stage)


class StageTimingAnalysisTests(unittest.TestCase):
    def test_nested_and_parallel_spans_do_not_double_count(self):
        def span(actor, phase, start, end):
            return {"Actor": actor, "Phase": phase, "StartNS": start, "EndNS": end}
        spans = [span("main", "model_request", 0, 400),
                 span("main", "collect_and_wait", 400, 700),
                 span("main", "verification", 600, 650),
                 span("main", "proposals_and_persist", 700, 750),
                 span("worker", "model_request", 100, 600)]
        got = stage.exclusive_wall(spans, 0, 1000)
        self.assertAlmostEqual(sum(got.values()), 0.000001)
        self.assertEqual(got["collect_and_wait"], 250 / 1e9)
        self.assertEqual(got["verification"], 50 / 1e9)
        self.assertEqual(got["runtime_or_unattributed"], 250 / 1e9)

    def test_compaction_outer_span_includes_api_once(self):
        spans = [{"Actor": "main", "Phase": "context_compaction", "StartNS": 10, "EndNS": 90},
                 {"Actor": "main", "Phase": "compaction_or_auxiliary_api", "StartNS": 20, "EndNS": 80}]
        got = stage.exclusive_wall(spans, 0, 100)
        self.assertEqual(got["context_compaction"], 80 / 1e9)
        self.assertNotIn("compaction_or_auxiliary_api", got)

    def test_nanosecond_time_and_worker_union(self):
        self.assertEqual(stage.iso_ns("2026-11-01T10:16:52.289007506Z") % 1_000_000_000, 289007506)
        self.assertEqual(stage.union_duration([(0, 10), (5, 15), (20, 30)]), 25 / 1e9)


if __name__ == "__main__":
    unittest.main()
