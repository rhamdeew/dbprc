import { FileButton, UnstyledButton } from '@mantine/core';
import { useRef } from 'react';
import { HEADER } from '../../theme/theme';

const IMAGE_ACCEPT = 'image/jpeg,image/png,image/gif,image/webp';

type FilePickerProps = {
  label: string;
  /** Accessible name of the hidden file input. */
  inputLabel: string;
  disabled?: boolean;
  onPick: (file: File) => void;
};

/** Link-styled control that opens the image file picker. */
export function FilePicker({ label, inputLabel, disabled = false, onPick }: FilePickerProps) {
  const resetRef = useRef<(() => void) | null>(null);
  return (
    <FileButton
      accept={IMAGE_ACCEPT}
      disabled={disabled}
      resetRef={resetRef}
      inputProps={{ 'aria-label': inputLabel }}
      onChange={(file) => {
        resetRef.current?.();
        if (file) onPick(file);
      }}
    >
      {(props) => (
        <UnstyledButton
          {...props}
          type="button"
          disabled={disabled}
          fz="sm"
          fw={600}
          style={{ color: HEADER, textDecoration: 'underline' }}
        >
          {label}
        </UnstyledButton>
      )}
    </FileButton>
  );
}
