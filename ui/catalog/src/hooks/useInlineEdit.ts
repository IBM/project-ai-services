import { useCallback, useEffect, useReducer, useRef } from "react";

interface UseInlineEditOptions {
  value: string;
  onSave: (newValue: string) => Promise<void>;
}

interface InlineEditState {
  editedValue: string;
  isEditing: boolean;
  isSaving: boolean;
  error: string;
  saveSuccess: boolean;
}

type InlineEditAction =
  | { type: "RESET"; value: string }
  | { type: "START_EDITING" }
  | { type: "CHANGE"; value: string }
  | { type: "SAVE_START" }
  | { type: "SAVE_SUCCESS"; value: string }
  | { type: "SAVE_FAILURE"; error: string }
  | { type: "CLEAR_ERROR" }
  | { type: "CLEAR_SUCCESS" };

// Builds the initial inline-edit state for the given persisted value
const createInitialState = (value: string): InlineEditState => ({
  editedValue: value,
  isEditing: false,
  isSaving: false,
  error: "",
  saveSuccess: false,
});

// Applies an inline-edit action to the current state
const inlineEditReducer = (
  state: InlineEditState,
  action: InlineEditAction,
): InlineEditState => {
  switch (action.type) {
    case "RESET":
      return {
        ...state,
        editedValue: action.value,
        isEditing: false,
        error: "",
      };
    case "START_EDITING":
      return { ...state, isEditing: true };
    case "CHANGE":
      return { ...state, editedValue: action.value, error: "" };
    case "SAVE_START":
      return { ...state, isSaving: true, error: "" };
    case "SAVE_SUCCESS":
      return {
        ...state,
        editedValue: action.value,
        isEditing: false,
        isSaving: false,
        saveSuccess: true,
      };
    case "SAVE_FAILURE":
      return { ...state, isSaving: false, error: action.error };
    case "CLEAR_ERROR":
      return { ...state, error: "" };
    case "CLEAR_SUCCESS":
      return { ...state, saveSuccess: false };
  }
};

// Manages inline-edit state (edit/save/cancel, error and success flags) for a single text value
export const useInlineEdit = ({ value, onSave }: UseInlineEditOptions) => {
  const [state, dispatch] = useReducer(
    inlineEditReducer,
    value,
    createInitialState,
  );
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    dispatch({ type: "RESET", value });
  }, [value]);

  useEffect(() => {
    if (state.isEditing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [state.isEditing]);

  const startEditing = useCallback(
    () => dispatch({ type: "START_EDITING" }),
    [],
  );

  const cancel = useCallback(() => dispatch({ type: "RESET", value }), [value]);

  const handleChange = useCallback(
    (newValue: string) => dispatch({ type: "CHANGE", value: newValue }),
    [],
  );

  const save = useCallback(async () => {
    const trimmedValue = state.editedValue.trim();
    if (!trimmedValue) return;
    dispatch({ type: "SAVE_START" });

    try {
      await onSave(trimmedValue);
      dispatch({ type: "SAVE_SUCCESS", value: trimmedValue });
    } catch (err) {
      dispatch({
        type: "SAVE_FAILURE",
        error: err instanceof Error ? err.message : "Please try again.",
      });
    }
  }, [state.editedValue, onSave]);

  const clearError = useCallback(() => dispatch({ type: "CLEAR_ERROR" }), []);
  const clearSuccess = useCallback(
    () => dispatch({ type: "CLEAR_SUCCESS" }),
    [],
  );

  return {
    editedValue: state.editedValue,
    isEditing: state.isEditing,
    isSaving: state.isSaving,
    error: state.error,
    saveSuccess: state.saveSuccess,
    inputRef,
    startEditing,
    cancel,
    handleChange,
    save,
    clearError,
    clearSuccess,
  };
};
