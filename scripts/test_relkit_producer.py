from __future__ import annotations

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import relkit_producer


class ReleaseCodeTests(unittest.TestCase):
    def test_matches_existing_dec_semver_strategy(self) -> None:
        self.assertEqual(relkit_producer.release_code("v1.13.133"), 1_013_133)
        self.assertEqual(relkit_producer.release_code("1.13.133+build"), 1_013_133)

    def test_rejects_ambiguous_components(self) -> None:
        with self.assertRaises(SystemExit):
            relkit_producer.release_code("v1.1000.0")


if __name__ == "__main__":
    unittest.main()
