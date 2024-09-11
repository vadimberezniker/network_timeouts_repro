Build the binary first (the name MUST be `repro`):
```
go build -o repro
```

Run the repro. Must be run as root to manipulate network namespaces.

```
sudo ./repro --subnet 10.100.0.0/16
```

The repro will run --concurrency concurrent goroutines that will make --n sequential GET requests to a test endpoint.
A --routines total number of goroutines will be started. Crank up --routines if you want the repro to run for a long time.




