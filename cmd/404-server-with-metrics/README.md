# Testing and Performance results for 404 default handler

## Setup
The testing was done a single host with the default 404 handler and the prometheus server running on the same machine. 

* Prometheus server
  * sudo apt-get install prometheus
  * sudo ./prometheus --log.level="info" --config.file="<path to config file>"
  * server running on localhost:9090
  * UI is available at http://localhost:9090
* 404 default handler service
  * running on localhost:8080
  * ${GOPATH}/src/k8s.io/ingress-gce/bin/amd64/404-server-with-metrics 

## Performance tests
Performance testing consisted of using Apache "ab" and "curl" to send lots of requests and monitor the metrics on prometheus UI

### Testing iterations

#### Testing with curl command

Ran curl script
```
for i in {1..100000}
do
  curl -s "http://localhost:8080/$i?[1-1000]"  > /tmp/load_test.output &
  curl -d "hola" -s "http://localhost:8080/$i?[1-1000]" &
done
```
* Results
  * default MAXGOPROCS = 12
  * around 20K requests/sec over 10m data 
  * max go routines in play : 10 (oscillates from 9 to 10)
  * server over 6M POST + 5M GET
  * server crashed with the following message

could not start http server or received shutdown: accept tcp [::]:8080: accept4: too many open files in system   

#### Testing with ab (1M requests with 100 concurrent connections)

```
ab -n 1000000 -c 100 -p /tmp/post.txt http://localhost:8080/page.html > /tmp/ab_post_test.log & \
ab -n 1000000 -c 100  http://localhost:8080/page.html > /tmp/ab_get_test.log & \
```
* Results
  * default MAXGOPROCS = 12
  * around 8K requests/sec each (GET and POST)over 2m 
  * max go routines peaked to 98 for 3m, steady state 10
  * server over 2M GET requests
```
Server Software:        
Server Hostname:        localhost
Server Port:            8080

Document Path:          /page.html
Document Length:        76 bytes

Concurrency Level:      100
Time taken for tests:   45.660 seconds
Complete requests:      1000000
Failed requests:        0
Non-2xx responses:      1000000
Total transferred:      200000000 bytes
HTML transferred:       76000000 bytes
Requests per second:    21901.13 [#/sec] (mean)
Time per request:       4.566 [ms] (mean)
Time per request:       0.046 [ms] (mean, across all concurrent requests)
Transfer rate:          4277.57 [Kbytes/sec] received

Connection Times (ms)
              min  mean[+/-sd] median   max
Connect:        0    2  10.2      2    1033
Processing:     0    2   0.5      2      14
Waiting:        0    2   0.5      2      12
Total:          0    5  10.2      4    1036

Percentage of the requests served within a certain time (ms)
  50%      4
  66%      5
  75%      5
  80%      5
  90%      5
  95%      5
  98%      6
  99%      6
 100%   1036 (longest request)
```

#### Testing with ab (10M requests with 10000 concurrent connections)

```
ab -n 10000000 -c 10000 -p /tmp/post.txt http://localhost:8080/page.html > /tmp/ab_post_test.log &
ab -n 10000000 -c 10000 http://localhost:8080/page.html > /tmp/ab_get_test.log &
```

* Results 
  * default MAXGOPROCS = 12
  * around 1K requests/sec each (GET and POST)over 2m  (combine throughput of 2K requests/sec)
  * max go routines peaked to 17K and oscillates between 5K to 15K 
  * server over 20M GET + POST requests

```
Benchmarking localhost (be patient)
(GET)

Server Software:
Server Hostname:        localhost
Server Port:            8080

Document Path:          /page.html
Document Length:        76 bytes

Concurrency Level:      10000
Time taken for tests:   10999.996 seconds
Complete requests:      10000000
Failed requests:        12678
   (Connect: 0, Receive: 0, Length: 6339, Exceptions: 6339)
Non-2xx responses:      9993661
Total transferred:      1998732200 bytes
HTML transferred:       759518236 bytes
Requests per second:    909.09 [#/sec] (mean)
Time per request:       10999.996 [ms] (mean)
Time per request:       1.100 [ms] (mean, across all concurrent requests)
Transfer rate:          177.44 [Kbytes/sec] received

Connection Times (ms)
              min  mean[+/-sd] median   max
Connect:        0 5480 2268.4   5496   12578
Processing:    83 5516 2268.6   5516   12537
Waiting:        0 2954 2025.5   1928    9443
Total:        360 10996 1150.0  11106   15559

Percentage of the requests served within a certain time (ms)
  50%  11106
  66%  11183
  75%  11231
  80%  11263
  90%  11352
  95%  11431
  98%  11517
  99%  11575
 100%  15559 (longest request)

Benchmarking localhost (be patient)
(POST)

Server Software:
Server Hostname:        localhost
Server Port:            8080

Document Path:          /page.html
Document Length:        76 bytes

Concurrency Level:      10000
Time taken for tests:   10997.479 seconds
Complete requests:      10000000
Failed requests:        0
Non-2xx responses:      10000000
Total transferred:      2000000000 bytes
Total body sent:        1530000000
HTML transferred:       760000000 bytes
Requests per second:    909.30 [#/sec] (mean)
Time per request:       10997.479 [ms] (mean)
Time per request:       1.100 [ms] (mean, across all concurrent requests)
Transfer rate:          177.60 [Kbytes/sec] received
                        135.86 kb/s sent
                        313.46 kb/s total

Connection Times (ms)
              min  mean[+/-sd] median   max
Connect:        0 5467 1426.8   5488   14194
Processing:     7 5529 1428.4   5557    8984
Waiting:        1 3585 868.2   3613    8570
Total:         22 10996 942.7  11082   16390

Percentage of the requests served within a certain time (ms)
  50%  11082
  66%  11160
  75%  11206
  80%  11236
  90%  11307
  95%  11370
  98%  11449
  99%  11511
 100%  16390 (longest request)
```

#### Testing with ab (10M requests with 2000 concurrent connections)

```
ab -n 10000000 -c 2000 http://localhost:8080/realtest > /tmp/ab_get_test.log
```
* Results
  * default MAXGOPROCS = 12
  * around 20K requests/sec (only PUTS) over 10m  
  * max go routines peaked to 15K
  * server over 10M PUT requests
  * request duration : 99%: 133us, 90%: 10us 

```
Benchmarking localhost (be patient)


Server Software:        
Server Hostname:        localhost
Server Port:            8080

Document Path:          /realtest
Document Length:        75 bytes

Concurrency Level:      2000
Time taken for tests:   500.674 seconds
Complete requests:      10000000
Failed requests:        0
Non-2xx responses:      10000000
Total transferred:      1990000000 bytes
HTML transferred:       750000000 bytes
Requests per second:    19973.06 [#/sec] (mean)
Time per request:       100.135 [ms] (mean)
Time per request:       0.050 [ms] (mean, across all concurrent requests)
Transfer rate:          3881.48 [Kbytes/sec] received

Connection Times (ms)
              min  mean[+/-sd] median   max
Connect:        0   48  30.0     47    1095
Processing:     5   52   8.4     52     175
Waiting:        0   36   9.7     35     159
Total:          5  100  29.7    101    1141

Percentage of the requests served within a certain time (ms)
  50%    101
  66%    102
  75%    103
  80%    103
  90%    105
  95%    107
  98%    110
  99%    111
 100%   1141 (longest request)
```

#### Testing with ab (20M requests with 2000 concurrent connections)

```
ab -n 10000000 -c 1000 -p /tmp/post.txt http://localhost:8080/page.html > /tmp/ab_post_test.log &
ab -n 10000000 -c 1000 http://localhost:8080/page.html > /tmp/ab_get_test.log &
```

* Results
  * default MAXGOPROCS = 12
  * around 18K requests/sec each (GET and POST) over 20m requests  (combine throughput of 36K requests/sec)
  * max go routines peaked to 1.5K and oscillates between 500 to 1.5K
  * server over 20M GET requests
  * http processing delay over 1m, low: 0.075ms, avg: 0.125ms and max: 0.2ms

```
Benchmarking localhost (be patient)


Server Software:        
Server Hostname:        localhost
Server Port:            8080

Document Path:          /page.html
Document Length:        76 bytes

Concurrency Level:      1000
Time taken for tests:   556.570 seconds
Complete requests:      10000000
Failed requests:        0
Non-2xx responses:      10000000
Total transferred:      2000000000 bytes
HTML transferred:       760000000 bytes
Requests per second:    17967.20 [#/sec] (mean)
Time per request:       55.657 [ms] (mean)
Time per request:       0.056 [ms] (mean, across all concurrent requests)
Transfer rate:          3509.22 [Kbytes/sec] received

Connection Times (ms)
              min  mean[+/-sd] median   max
Connect:        0   29  67.3     25    3072
Processing:     0   27   7.4     27     237
Waiting:        0   18   6.8     17     231
Total:          0   56  67.6     55    3105

Percentage of the requests served within a certain time (ms)
  50%     55
  66%     56
  75%     57
  80%     57
  90%     58
  95%     59
  98%     61
  99%     64
 100%   3105 (longest request)

Benchmarking localhost (be patient)


Server Software:        
Server Hostname:        localhost
Server Port:            8080

Document Path:          /page.html
Document Length:        76 bytes

Concurrency Level:      1000
Time taken for tests:   557.359 seconds
Complete requests:      10000000
Failed requests:        0
Non-2xx responses:      10000000
Total transferred:      2000000000 bytes
Total body sent:        1530000000
HTML transferred:       760000000 bytes
Requests per second:    17941.77 [#/sec] (mean)
Time per request:       55.736 [ms] (mean)
Time per request:       0.056 [ms] (mean, across all concurrent requests)
Transfer rate:          3504.25 [Kbytes/sec] received
                        2680.75 kb/s sent
                        6185.01 kb/s total

Connection Times (ms)
              min  mean[+/-sd] median   max
Connect:        0   29  70.2     25    3075
Processing:     0   27   8.2     27     871
Waiting:        0   18   7.4     18     869
Total:          0   56  71.1     55    3105

Percentage of the requests served within a certain time (ms)
  50%     55
  66%     56
  75%     57
  80%     57
  90%     58
  95%     59
  98%     61
  99%     64
 100%   3105 (longest request)
```
