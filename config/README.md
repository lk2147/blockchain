# Node configuration

Generate working configurations and fresh private keys with Python, from the
repository root:

```text
python tests/run_local_cluster.py --run-dir local-runs/manual --prepare-only
```

Edit the resulting node-00.json through node-09.json before starting processes.
The root README explains individual terminals, all-node launches and fault fields;
tests/REAL_NETWORK.md defines the transport and complete configuration controls.
Configs and keys belong together in that fresh local directory. No development
private keys are distributed in the submission. Fixed test-only identities are
created explicitly by the Go test fixtures, independently of the fault seed.

Keep ChainID, Peers and workload consistent across a cluster. Each node needs its
own NodeID, corresponding private key, listen address and output directory. Ten
validators give f=3 and a seven-signature quorum. Use loopback addresses only.
Faults can differ per node; changing a fault seed never changes cryptographic keys.
