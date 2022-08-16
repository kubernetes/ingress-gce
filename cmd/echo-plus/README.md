# Echo-Plus

Echo-Plus is a workload application for NEG transition testing.

## Functions

Echo-Plus has the following functions


| Path                   | Method       | Description                                                                                                              |
|------------------------|--------------|--------------------------------------------------------------------------------------------------------------------------|
| /healthcheck           | GET          | Checks whether the workload is healthy                                                                                   |
| /panic                 | GET          | Crashes the app running on a pod with os.Exit(1)                                                                         |
| /slowRequest/{latency} | GET          | Checks whether the workload is healthy after a latency.                                                                  |
| /randomSlowRequest     | GET          | Checks whether the workload is healthy after a random latency. The random seed is initialized based on the current time. |
| /testMethod            | GET PUT POST | Sends different http method calls to the workload                                                                        |
| /                      | GET          | Retrieves info about the workload and k8s environment                                                                    |