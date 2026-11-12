# Configuration

Generate fresh keys, network.json and compact per-node configs:

```text
python tests/run_local_cluster.py --run-dir local-runs/demo --run-ms 0 --prepare-only
```

The root README documents defaults, Behavior, outbound limits, client submission
and one-node-per-terminal commands. Generated private keys and data stay under
ignored local-runs. No pre-generated transactions belong in node configuration.
