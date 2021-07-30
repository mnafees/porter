import React, {
  Component,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import styled from "styled-components";
import api from "shared/api";
import { Context } from "shared/Context";
import { ChartType } from "shared/types";
import ResourceTab from "components/ResourceTab";
import ConfirmOverlay from "components/ConfirmOverlay";
import { useWebsockets } from "shared/hooks/useWebsockets";
import PodRow from "./PodRow";

type PropsType = {
  controller: any;
  selectedPod: any;
  selectPod: Function;
  selectors: any;
  isLast?: boolean;
  isFirst?: boolean;
  setPodError: (x: string) => void;
};

type StateType = {
  pods: any[];
  raw: any[];
  showTooltip: boolean[];
  podPendingDelete: any;
  websockets: Record<string, any>;
  selectors: string[];
  available: number;
  total: number;
  canUpdatePod: boolean;
};

// Controller tab in log section that displays list of pods on click.
class ControllerTab extends Component<PropsType, StateType> {
  state = {
    pods: [] as any[],
    raw: [] as any[],
    showTooltip: [] as boolean[],
    podPendingDelete: null as any,
    websockets: {} as Record<string, any>,
    selectors: [] as string[],
    available: null as number,
    total: null as number,
    canUpdatePod: true,
  };

  updatePods = () => {
    let { currentCluster, currentProject, setCurrentError } = this.context;
    let { controller, selectPod, isFirst } = this.props;

    api
      .getMatchingPods(
        "<token>",
        {
          cluster_id: currentCluster.id,
          namespace: controller?.metadata?.namespace,
          selectors: this.state.selectors,
        },
        {
          id: currentProject.id,
        }
      )
      .then((res) => {
        let pods = res?.data?.map((pod: any) => {
          return {
            namespace: pod?.metadata?.namespace,
            name: pod?.metadata?.name,
            phase: pod?.status?.phase,
          };
        });
        let showTooltip = new Array(pods.length);
        for (let j = 0; j < pods.length; j++) {
          showTooltip[j] = false;
        }

        this.setState({ pods, raw: res.data, showTooltip });

        if (isFirst) {
          let pod = res.data[0];
          let status = this.getPodStatus(pod.status);
          status === "failed" &&
            pod.status?.message &&
            this.props.setPodError(pod.status?.message);
          if (this.state.canUpdatePod) {
            // this prevents multiple requests from changing the first pod
            selectPod(res.data[0]);
            this.setState({
              canUpdatePod: false,
            });
          }
        }
      })
      .catch((err) => {
        console.log(err);
        setCurrentError(JSON.stringify(err));
        return;
      });
  };

  getPodSelectors = (callback: () => void) => {
    let { controller } = this.props;

    let selectors = [] as string[];
    let ml =
      controller?.spec?.selector?.matchLabels || controller?.spec?.selector;
    let i = 1;
    let selector = "";
    for (var key in ml) {
      selector += key + "=" + ml[key];
      if (i != Object.keys(ml).length) {
        selector += ",";
      }
      i += 1;
    }
    selectors.push(selector);
    if (controller.kind.toLowerCase() == "job" && this.props.selectors) {
      selectors = this.props.selectors;
    }

    this.setState({ selectors }, () => {
      callback();
    });
  };

  componentDidMount() {
    this.getPodSelectors(() => {
      this.updatePods();
      this.setControllerWebsockets([this.props.controller.kind, "pod"]);
    });
  }

  componentWillUnmount() {
    if (this.state.websockets) {
      this.state.websockets.forEach((ws: WebSocket) => {
        ws.close();
      });
    }
  }

  setControllerWebsockets = (controller_types: any[]) => {
    let websockets = controller_types.map((kind: string) => {
      return this.setupWebsocket(kind);
    });
    this.setState({ websockets });
  };

  setupWebsocket = (kind: string) => {
    let { currentCluster, currentProject } = this.context;
    let protocol = window.location.protocol == "https:" ? "wss" : "ws";
    let connString = `${protocol}://${window.location.host}/api/projects/${currentProject.id}/k8s/${kind}/status?cluster_id=${currentCluster.id}`;

    if (kind == "pod" && this.state.selectors) {
      connString += `&selectors=${this.state.selectors[0]}`;
    }
    let ws = new WebSocket(connString);

    ws.onopen = () => {
      console.log("connected to websocket");
    };

    ws.onmessage = (evt: MessageEvent) => {
      let event = JSON.parse(evt.data);
      let object = event.Object;
      object.metadata.kind = event.Kind;

      // update pods no matter what if ws message is a pod event.
      // If controller event, check if ws message corresponds to the designated controller in props.
      if (
        event.Kind != "pod" &&
        object.metadata.uid != this.props.controller.metadata.uid
      )
        return;

      if (event.Kind != "pod") {
        let [available, total] = this.getAvailability(
          object.metadata.kind,
          object
        );
        this.setState({ available, total });
      }

      this.updatePods();
    };

    ws.onclose = () => {
      console.log("closing websocket");
    };

    ws.onerror = (err: ErrorEvent) => {
      console.log(err);
      ws.close();
    };

    return ws;
  };

  getAvailability = (kind: string, c: any) => {
    switch (kind?.toLowerCase()) {
      case "deployment":
      case "replicaset":
        return [
          c.status?.availableReplicas ||
            c.status?.replicas - c.status?.unavailableReplicas ||
            0,
          c.status?.replicas || 0,
        ];
      case "statefulset":
        return [c.status?.readyReplicas || 0, c.status?.replicas || 0];
      case "daemonset":
        return [
          c.status?.numberAvailable || 0,
          c.status?.desiredNumberScheduled || 0,
        ];
      case "job":
        return [1, 1];
    }
  };

  getPodStatus = (status: any) => {
    if (
      status?.phase === "Pending" &&
      status?.containerStatuses !== undefined
    ) {
      return status.containerStatuses[0].state.waiting.reason;
    } else if (status?.phase === "Pending") {
      return "Pending";
    }

    if (status?.phase === "Failed") {
      return "failed";
    }

    if (status?.phase === "Running") {
      let collatedStatus = "running";

      status?.containerStatuses?.forEach((s: any) => {
        if (s.state?.waiting) {
          collatedStatus =
            s.state?.waiting.reason === "CrashLoopBackOff"
              ? "failed"
              : "waiting";
        } else if (s.state?.terminated) {
          collatedStatus = "failed";
        }
      });
      return collatedStatus;
    }
  };

  renderTooltip = (x: string, ind: number): JSX.Element | undefined => {
    if (this.state.showTooltip[ind]) {
      return <Tooltip>{x}</Tooltip>;
    }
  };

  handleDeletePod = (pod: any) => {
    api
      .deletePod(
        "<token>",
        {
          cluster_id: this.context.currentCluster.id,
        },
        {
          name: pod.metadata?.name,
          namespace: pod.metadata?.namespace,
          id: this.context.currentProject.id,
        }
      )
      .then((res) => {
        this.updatePods();
        this.setState({ podPendingDelete: null });
      })
      .catch((err) => {
        this.context.setCurrentError(JSON.stringify(err));
        this.setState({ podPendingDelete: null });
      });
  };

  renderDeleteButton = (pod: any) => {
    return (
      <CloseIcon
        className="material-icons-outlined"
        onClick={() => this.setState({ podPendingDelete: pod })}
      >
        close
      </CloseIcon>
    );
  };

  // render() {
  //   let { controller, selectedPod, isLast, selectPod, isFirst } = this.props;
  //   let { available, total } = this.state;
  //   let status = available == total ? "running" : "waiting";

  //   controller?.status?.conditions?.forEach((condition: any) => {
  //     if (
  //       condition.type == "Progressing" &&
  //       condition.status == "False" &&
  //       condition.reason == "ProgressDeadlineExceeded"
  //     ) {
  //       status = "failed";
  //     }
  //   });

  //   if (controller.kind.toLowerCase() === "job" && this.state.raw.length == 0) {
  //     status = "completed";
  //   }

  //   return (
  //     <ResourceTab
  //       label={controller.kind}
  //       // handle CronJob case
  //       name={controller.metadata?.name || controller.name}
  //       status={{ label: status, available, total }}
  //       isLast={isLast}
  //       expanded={isFirst}
  //     >
  //       {this.state.raw.map((pod, i) => {
  //         let status = this.getPodStatus(pod.status);
  //         return (
  //           <Tab
  //             key={pod.metadata?.name}
  //             selected={selectedPod?.metadata?.name === pod?.metadata?.name}
  //             onClick={() => {
  //               this.props.setPodError("");
  //               status === "failed" &&
  //                 pod.status?.message &&
  //                 this.props.setPodError(pod.status?.message);
  //               selectPod(pod);
  //               this.setState({
  //                 canUpdatePod: false,
  //               });
  //             }}
  //           >
  //             <Gutter>
  //               <Rail />
  //               <Circle />
  //               <Rail lastTab={i === this.state.raw.length - 1} />
  //             </Gutter>
  //             <Name
  //               onMouseOver={() => {
  //                 let showTooltip = this.state.showTooltip;
  //                 showTooltip[i] = true;
  //                 this.setState({ showTooltip });
  //               }}
  //               onMouseOut={() => {
  //                 let showTooltip = this.state.showTooltip;
  //                 showTooltip[i] = false;
  //                 this.setState({ showTooltip });
  //               }}
  //             >
  //               {pod.metadata?.name}
  //             </Name>
  //             {this.renderTooltip(pod.metadata?.name, i)}
  //             <Status>
  //               <StatusColor status={status} />
  //               {status}
  //               {status === "failed" && this.renderDeleteButton(pod)}
  //             </Status>
  //           </Tab>
  //         );
  //       })}
  //       <ConfirmOverlay
  //         message="Are you sure you want to delete this pod?"
  //         show={this.state.podPendingDelete}
  //         onYes={() => this.handleDeletePod(this.state.podPendingDelete)}
  //         onNo={() => this.setState({ podPendingDelete: null })}
  //       />
  //     </ResourceTab>
  //   );
  // }
}

ControllerTab.contextType = Context;

// export default ControllerTab;

export type ControllerTabPodType = {
  namespace: string;
  name: string;
  phase: string;
  status: any;
  replicaSetName: string;
};

const ControllerTabFC: React.FunctionComponent<PropsType> = ({
  controller,
  selectPod,
  isFirst,
  isLast,
  selectors,
  setPodError,
  selectedPod,
}) => {
  const [pods, setPods] = useState<ControllerTabPodType[]>([]);
  const [podPendingDelete, setPodPendingDelete] = useState<any>(null);
  const [available, setAvailable] = useState<number>(null);
  const [total, setTotal] = useState<number>(null);
  const [userSelectedPod, setUserSelectedPod] = useState<boolean>(false);

  const { currentCluster, currentProject, setCurrentError } = useContext(
    Context
  );
  const {} = useWebsockets();

  const currentSelectors = useMemo(() => {
    if (controller.kind.toLowerCase() == "job" && selectors) {
      return [...selectors];
    }
    let newSelectors = [] as string[];
    let ml =
      controller?.spec?.selector?.matchLabels || controller?.spec?.selector;
    let i = 1;
    let selector = "";
    for (var key in ml) {
      selector += key + "=" + ml[key];
      if (i != Object.keys(ml).length) {
        selector += ",";
      }
      i += 1;
    }
    newSelectors.push(selector);
    return [...newSelectors];
  }, [controller, selectors]);

  const updatePods = async () => {
    try {
      const res = await api.getMatchingPods(
        "<token>",
        {
          cluster_id: currentCluster.id,
          namespace: controller?.metadata?.namespace,
          selectors: currentSelectors,
        },
        {
          id: currentProject.id,
        }
      );
      const data = res?.data as any[];
      let newPods = data
        // Parse only data that we need
        .map<ControllerTabPodType>((pod: any) => {
          const replicaSetName =
            Array.isArray(pod?.metadata?.ownerReferences) &&
            pod?.metadata?.ownerReferences[0]?.name;
          return {
            namespace: pod?.metadata?.namespace,
            name: pod?.metadata?.name,
            phase: pod?.status?.phase,
            status: pod?.status,
            replicaSetName,
          };
        });

      setPods(newPods);
      if (!userSelectedPod) {
        let status = getPodStatus(newPods[0].status);
        status === "failed" &&
          newPods[0].status?.message &&
          setPodError(newPods[0].status?.message);
        selectPod(newPods[0]);
      }
    } catch (error) {}
  };

  useEffect(() => {
    updatePods();
  }, [currentSelectors, controller, currentCluster, currentProject]);

  const currentControllerStatus = useMemo(() => {
    let status = available == total ? "running" : "waiting";

    controller?.status?.conditions?.forEach((condition: any) => {
      if (
        condition.type == "Progressing" &&
        condition.status == "False" &&
        condition.reason == "ProgressDeadlineExceeded"
      ) {
        status = "failed";
      }
    });

    if (controller.kind.toLowerCase() === "job" && pods.length == 0) {
      status = "completed";
    }
    return status;
  }, [controller, available, total, pods]);

  const getPodStatus = (status: any) => {
    if (
      status?.phase === "Pending" &&
      status?.containerStatuses !== undefined
    ) {
      return status.containerStatuses[0].state.waiting.reason;
    } else if (status?.phase === "Pending") {
      return "Pending";
    }

    if (status?.phase === "Failed") {
      return "failed";
    }

    if (status?.phase === "Running") {
      let collatedStatus = "running";

      status?.containerStatuses?.forEach((s: any) => {
        if (s.state?.waiting) {
          collatedStatus =
            s.state?.waiting.reason === "CrashLoopBackOff"
              ? "failed"
              : "waiting";
        } else if (s.state?.terminated) {
          collatedStatus = "failed";
        }
      });
      return collatedStatus;
    }
  };

  const handleDeletePod = (pod: any) => {
    api
      .deletePod(
        "<token>",
        {
          cluster_id: currentCluster.id,
        },
        {
          name: pod.metadata?.name,
          namespace: pod.metadata?.namespace,
          id: currentProject.id,
        }
      )
      .then((res) => {
        updatePods();
        setPodPendingDelete(null);
      })
      .catch((err) => {
        setCurrentError(JSON.stringify(err));
        setPodPendingDelete(null);
      });
  };

  const replicaSetArray = useMemo(() => {
    const podsDividedByReplicaSet = pods.reduce<
      Array<Array<ControllerTabPodType>>
    >(function (prev, currentPod, i) {
      if (
        !i ||
        prev[prev.length - 1][0].replicaSetName !== currentPod.replicaSetName
      ) {
        return prev.concat([[currentPod]]);
      }
      prev[prev.length - 1].push(currentPod);
      return prev;
    }, []);

    if (podsDividedByReplicaSet.length === 1) {
      return [];
    } else {
      return podsDividedByReplicaSet;
    }
  }, [pods]);

  return (
    <ResourceTab
      label={controller.kind}
      // handle CronJob case
      name={controller.metadata?.name || controller.name}
      status={{ label: currentControllerStatus, available, total }}
      isLast={isLast}
      expanded={isFirst}
    >
      {pods.map((pod, i) => {
        let status = getPodStatus(pod.status);
        return (
          <PodRow
            key={i}
            pod={pod}
            isSelected={selectedPod?.name === pod?.name}
            podStatus={status}
            isLastItem={i === pods.length - 1}
            onTabClick={() => {
              setPodError("");
              status === "failed" &&
                pod.status?.message &&
                setPodError(pod.status?.message);
              selectPod(pod);
              setUserSelectedPod(true);
            }}
            onDeleteClick={() => setPodPendingDelete(pod)}
          />
        );
      })}
      <ConfirmOverlay
        message="Are you sure you want to delete this pod?"
        show={podPendingDelete}
        onYes={() => handleDeletePod(podPendingDelete)}
        onNo={() => setPodPendingDelete(null)}
      />
    </ResourceTab>
  );
};

export default ControllerTabFC;

const CloseIcon = styled.i`
  font-size: 14px;
  display: flex;
  font-weight: bold;
  align-items: center;
  justify-content: center;
  border-radius: 5px;
  background: #ffffff22;
  width: 18px;
  height: 18px;
  margin-right: -6px;
  margin-left: 10px;
  cursor: pointer;
  :hover {
    background: #ffffff44;
  }
`;

const Tooltip = styled.div`
  position: absolute;
  left: 35px;
  word-wrap: break-word;
  top: 38px;
  min-height: 18px;
  max-width: calc(100% - 75px);
  padding: 2px 5px;
  background: #383842dd;
  display: flex;
  justify-content: center;
  flex: 1;
  color: white;
  text-transform: none;
  font-size: 12px;
  font-family: "Work Sans", sans-serif;
  outline: 1px solid #ffffff55;
  opacity: 0;
  animation: faded-in 0.2s 0.15s;
  animation-fill-mode: forwards;
  @keyframes faded-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
`;
