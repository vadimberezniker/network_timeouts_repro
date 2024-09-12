The test machine must have ip forwarding & NAT enabled as the repro uses private IPs to send traffic to an external endpoint.

Build the binary first (the name MUST be `repro`):
```
go build -o repro
```

Run the repro. Must be run as root to manipulate network namespaces. Adjust the subnet as necessary for your test machine.

```
sudo ./repro --subnet 10.100.0.0/16
```

The repro will run --concurrency concurrent goroutines that will make --n sequential GET requests to a test endpoint.

After you are done the following command to clean up network namespaces and veth devices:

```
sudo ./repro --subnet 10.100.0.0/16 -cleanup
```




