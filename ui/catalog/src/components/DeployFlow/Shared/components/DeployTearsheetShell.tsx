import { Tearsheet } from "@carbon/ibm-products";
import {
  ProgressIndicator,
  ProgressStep,
  InlineLoading,
  ActionableNotification,
} from "@carbon/react";
import styles from "../DeployFlow.shared.module.scss";

export interface StepDefinition {
  label: string;
  description: string;
  complete?: boolean;
}

interface DeployTearsheetShellProps {
  open: boolean;
  onClose: () => void;
  title: string;
  steps: StepDefinition[];
  currentStep: number;
  isLastStep: boolean;
  isDeploying: boolean;
  isPrimaryDisabled: boolean;
  onBack: () => void;
  onNext: () => void;
  onSubmit: () => void;
  deployError: string | null;
  deployToastOpen: boolean;
  onRetryDeploy: () => void;
  onDismissToast: () => void;
  isLoading?: boolean;
  error?: string | null;
  children: React.ReactNode;
}

export const DeployTearsheetShell = ({
  open,
  onClose,
  title,
  steps,
  currentStep,
  isLastStep,
  isDeploying,
  isPrimaryDisabled,
  onBack,
  onNext,
  onSubmit,
  deployError,
  deployToastOpen,
  onRetryDeploy,
  onDismissToast,
  isLoading = false,
  error = null,
  children,
}: DeployTearsheetShellProps) => {
  const actions = [
    {
      label: "Cancel",
      kind: "ghost" as const,
      onClick: onClose,
      disabled: isDeploying,
    },
    {
      label: "Back",
      kind: "secondary" as const,
      onClick: onBack,
      disabled: currentStep === 0 || isDeploying,
    },
    {
      label: isLastStep ? (isDeploying ? "Deploying..." : "Deploy") : "Next",
      kind: "primary" as const,
      onClick: isLastStep ? onSubmit : onNext,
      disabled: isPrimaryDisabled || isDeploying,
    },
  ];

  return (
    <>
      {deployToastOpen && deployError && (
        <ActionableNotification
          actionButtonLabel="Try again"
          aria-label="close notification"
          kind="error"
          closeOnEscape
          title="Deployment failed"
          subtitle={deployError}
          onCloseButtonClick={onDismissToast}
          onActionButtonClick={onRetryDeploy}
          className={styles.deployErrorNotification}
        />
      )}
      <Tearsheet
        open={open}
        onClose={onClose}
        title={title}
        actions={actions}
        className="customTearsheet"
        influencer={
          <div className={styles.influencerContent}>
            <ProgressIndicator currentIndex={currentStep} vertical>
              {steps.map((step) => (
                <ProgressStep
                  key={step.label}
                  label={step.label}
                  description={step.description}
                  complete={step.complete}
                />
              ))}
            </ProgressIndicator>
          </div>
        }
        influencerPosition="left"
        influencerWidth="narrow"
      >
        <div className={styles.stepContent}>
          {isLoading ? (
            <div className={styles.loadingContainer}>
              <InlineLoading description="Loading deploy options..." />
            </div>
          ) : error ? (
            <div className={styles.errorContainer}>
              <p>Error: {error}</p>
            </div>
          ) : (
            children
          )}
        </div>
      </Tearsheet>
    </>
  );
};
