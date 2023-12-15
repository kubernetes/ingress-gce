| Node State     | State Definition                         | ProviderID | Controller Processes/Uses the Node |
| -------------- | ---------------------------------------- | ---------- | ---------------------------------- |
| Node Created   |  k8s node object created                 | Empty      | No                                 |
| Node Processed |  ProviderID and Topology Label populated | Set        | Yes                                |
| Node Ready     |  NodeReady condition set to true         | Set        | Yes                                |