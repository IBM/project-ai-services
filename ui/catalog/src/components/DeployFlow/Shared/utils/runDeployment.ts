import { SHARED_ACTION_TYPES } from "../types";
import type { SharedDeployFlowAction } from "../types";
import { extractDeployError } from "./deployError";

interface RunDeploymentOptions {
  dispatch: React.Dispatch<SharedDeployFlowAction>;
  deploy: () => Promise<void>;
  onSuccess: () => void;
}

export async function runDeployment({
  dispatch,
  deploy,
  onSuccess,
}: RunDeploymentOptions): Promise<void> {
  dispatch({ type: SHARED_ACTION_TYPES.SET_IS_DEPLOYING, payload: true });
  dispatch({ type: SHARED_ACTION_TYPES.SET_DEPLOY_ERROR, payload: null });
  dispatch({ type: SHARED_ACTION_TYPES.HIDE_DEPLOY_TOAST });

  try {
    await deploy();
    onSuccess(); // onSuccess dispatches RESET_STATE which clears isDeploying via state reset
  } catch (error: unknown) {
    dispatch({
      type: SHARED_ACTION_TYPES.SET_DEPLOY_ERROR,
      payload: extractDeployError(error),
    });
    dispatch({ type: SHARED_ACTION_TYPES.SHOW_DEPLOY_TOAST });
    dispatch({ type: SHARED_ACTION_TYPES.SET_IS_DEPLOYING, payload: false });
  }
}
