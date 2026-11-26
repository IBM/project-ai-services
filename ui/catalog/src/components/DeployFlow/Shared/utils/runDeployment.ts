import { SHARED_ACTION_TYPES } from "../types";
import type { SharedDeployFlowAction } from "../types";

interface RunDeploymentOptions {
  dispatch: React.Dispatch<SharedDeployFlowAction>;
  deploy: () => Promise<void>;
  onSuccess: () => void;
  fallbackError?: string;
}

export async function runDeployment({
  dispatch,
  deploy,
  onSuccess,
  fallbackError = "Failed to deploy application",
}: RunDeploymentOptions): Promise<void> {
  dispatch({ type: SHARED_ACTION_TYPES.SET_IS_DEPLOYING, payload: true });
  dispatch({ type: SHARED_ACTION_TYPES.SET_DEPLOY_ERROR, payload: null });
  dispatch({ type: SHARED_ACTION_TYPES.HIDE_DEPLOY_TOAST });

  try {
    await deploy();
    onSuccess();
  } catch (error: unknown) {
    dispatch({
      type: SHARED_ACTION_TYPES.SET_DEPLOY_ERROR,
      payload: error instanceof Error ? error.message : fallbackError,
    });
    dispatch({ type: SHARED_ACTION_TYPES.SHOW_DEPLOY_TOAST });
  } finally {
    dispatch({ type: SHARED_ACTION_TYPES.SET_IS_DEPLOYING, payload: false });
  }
}
