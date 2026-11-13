import { Progress, Text } from '@mantine/core';

type UploadProgressProps = {
  value: number | null;
  label: string;
};

export function UploadProgress({ value, label }: UploadProgressProps) {
  if (value === null) return null;
  return (
    <>
      <Text fz="xs">Uploading... {value}%</Text>
      <Progress value={value} animated={value < 100} aria-label={label} size="sm" color="arcBlue" />
    </>
  );
}
