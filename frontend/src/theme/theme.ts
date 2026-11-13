import { createTheme, type CSSVariablesResolver, type MantineColorsTuple } from '@mantine/core';

/** Light-blue page panel used behind the centered column. */
export const PANEL = '#edeff4';
/** Hairline used on panels and the top bar. */
export const BORDER = '#d8dfea';
/** Top bar blue. Shade 6 of `arcBlue`. */
export const HEADER = '#3b5998';
/** Darker edge on the top bar and on primary buttons. */
export const HEADER_DARK = '#29447e';
/** Page background. Panels sit on it with a `BORDER` hairline. */
export const PAGE = '#ffffff';
/** My own chat bubble. */
export const BUBBLE = '#e3e8f2';
export const BUBBLE_BORDER = '#c9d2e3';
/** Bevel on a primary call-to-action button. */
export const BEVEL = { border: `1px solid ${HEADER_DARK}`, boxShadow: 'inset 0 1px 0 rgba(255, 255, 255, 0.3)' };
/** Centered column every page sits in. */
export const CONTENT_WIDTH = 980;

const arcBlue: MantineColorsTuple = [
  PANEL,
  BORDER,
  '#b4c2db',
  '#8fa4cb',
  '#6b86bb',
  '#4c6aaa',
  HEADER,
  '#2f4779',
  '#23355b',
  '#17233c',
];

const flat = 'none';

// "2006 social network" look: dense type, square corners, no drop shadows.
export const theme = createTheme({
  primaryColor: 'arcBlue',
  primaryShade: 6,
  colors: { arcBlue },
  white: '#ffffff',
  black: '#333333',
  fontFamily: 'Tahoma, Verdana, sans-serif',
  fontFamilyMonospace: 'Tahoma, Verdana, sans-serif',
  headings: {
    fontFamily: 'Tahoma, Verdana, sans-serif',
    fontWeight: '700',
    sizes: {
      h1: { fontSize: '18px', fontWeight: '700', lineHeight: '1.3' },
      h2: { fontSize: '16px', fontWeight: '700', lineHeight: '1.35' },
      h3: { fontSize: '14px', fontWeight: '700', lineHeight: '1.4' },
      h4: { fontSize: '13px', fontWeight: '700', lineHeight: '1.4' },
      h5: { fontSize: '12px', fontWeight: '700', lineHeight: '1.45' },
      h6: { fontSize: '12px', fontWeight: '700', lineHeight: '1.45' },
    },
  },
  fontSizes: {
    xs: '11px',
    sm: '12px',
    md: '12px',
    lg: '14px',
    xl: '16px',
  },
  lineHeights: {
    xs: '1.4',
    sm: '1.45',
    md: '1.45',
    lg: '1.5',
    xl: '1.55',
  },
  defaultRadius: 2,
  radius: {
    xs: '2px',
    sm: '2px',
    md: '2px',
    lg: '2px',
    xl: '2px',
  },
  shadows: {
    xs: flat,
    sm: flat,
    md: flat,
    lg: flat,
    xl: flat,
  },
  cursorType: 'pointer',
  other: {
    panel: PANEL,
    border: BORDER,
    header: HEADER,
    contentWidth: `${CONTENT_WIDTH}px`,
  },
  components: {
    Paper: { defaultProps: { shadow: flat, radius: 2 } },
    Card: { defaultProps: { shadow: flat, radius: 2 } },
    Modal: { defaultProps: { shadow: flat, radius: 2 } },
    Popover: { defaultProps: { shadow: flat, radius: 2 } },
    Menu: { defaultProps: { shadow: flat } },
    Tooltip: { defaultProps: { shadow: flat, radius: 2 } },
    // Dense controls. A call site can still pass its own size.
    Button: { defaultProps: { size: 'xs' } },
    TextInput: { defaultProps: { size: 'xs' } },
    Textarea: { defaultProps: { size: 'xs' } },
    Checkbox: { defaultProps: { size: 'xs' } },
    Switch: { defaultProps: { size: 'xs' } },
  },
});

export const cssVariablesResolver: CSSVariablesResolver = () => ({
  variables: {
    '--arc-panel': PANEL,
    '--arc-border': BORDER,
    '--arc-header': HEADER,
    '--arc-content-width': `${CONTENT_WIDTH}px`,
  },
  light: {
    '--mantine-color-body': PAGE,
    '--mantine-color-text': '#333333',
    '--mantine-color-default-border': BORDER,
  },
  dark: {
    '--mantine-color-body': PAGE,
    '--mantine-color-text': '#333333',
    '--mantine-color-default-border': BORDER,
  },
});
