import { useReducer, useCallback } from "react";
import { PageHeader } from "@carbon/ibm-products";
import WorkerResourcesTable from "@/components/WorkerResourcesTable";
import RegisterWorkerModal from "@/components/WorkerResourcesTable/RegisterWorkerModal";
import { registerWorker } from "@/api/workerResources.api";
import { workerResourcesReducer, initialState } from "./types";

const WorkerResources = () => {
  const [state, dispatch] = useReducer(workerResourcesReducer, initialState);

  const handleGenerateToken = useCallback(async () => {
    if (!state.workerName.trim()) {
      dispatch({ type: "SET_PHASE", payload: "invalid" });
      return;
    }
    dispatch({ type: "SET_PHASE", payload: "loading" });
    try {
      const result = await registerWorker(state.workerName.trim());
      dispatch({ type: "REGISTER_SUCCESS", payload: { token: result.token } });
    } catch (err) {
      dispatch({
        type: "REGISTER_ERROR",
        payload:
          err instanceof Error ? err.message : "Failed to register worker",
      });
    }
  }, [state.workerName]);

  return (
    <>
      <PageHeader title="Worker Resources" />
      <WorkerResourcesTable
        onRegister={() => dispatch({ type: "OPEN_MODAL" })}
        refreshTrigger={state.refreshTrigger}
      />
      <RegisterWorkerModal
        isOpen={state.isModalOpen}
        phase={state.phase}
        workerName={state.workerName}
        token={state.token}
        errorMessage={state.errorMessage}
        onWorkerNameChange={(value) =>
          dispatch({ type: "SET_WORKER_NAME", payload: value })
        }
        onGenerateToken={() => void handleGenerateToken()}
        onClose={() => dispatch({ type: "CLOSE_MODAL" })}
      />
    </>
  );
};

export default WorkerResources;
