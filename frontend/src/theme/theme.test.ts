import { describe, expect, it } from 'vitest';
import { BORDER, HEADER, PANEL, cssVariablesResolver, theme } from './theme';

describe('2006 theme', () => {
  it('uses the social-network palette, 12px Tahoma, 2px radius and no shadows', () => {
    expect(theme.primaryColor).toBe('arcBlue');
    expect(theme.primaryShade).toBe(6);
    expect(theme.colors?.arcBlue?.[0]).toBe(PANEL);
    expect(theme.colors?.arcBlue?.[1]).toBe(BORDER);
    expect(theme.colors?.arcBlue?.[6]).toBe(HEADER);
    expect(theme.colors?.arcBlue?.[6]).toBe('#3b5998');
    expect(PANEL).toBe('#edeff4');
    expect(BORDER).toBe('#d8dfea');
    expect(theme.fontFamily).toBe('Tahoma, Verdana, sans-serif');
    expect(theme.headings?.fontFamily).toBe('Tahoma, Verdana, sans-serif');
    expect(theme.fontSizes?.md).toBe('12px');
    expect(theme.defaultRadius).toBe(2);
    expect(theme.radius).toMatchObject({ xs: '2px', sm: '2px', md: '2px', lg: '2px', xl: '2px' });
    expect(theme.shadows).toEqual({ xs: 'none', sm: 'none', md: 'none', lg: 'none', xl: 'none' });
    expect(theme.components?.Paper?.defaultProps?.shadow).toBe('none');
    expect(theme.components?.Button?.defaultProps?.size).toBe('xs');
  });

  it('paints a white page and the default border', () => {
    const vars = cssVariablesResolver({} as never);
    expect(vars.variables['--arc-panel']).toBe('#edeff4');
    expect(vars.variables['--arc-border']).toBe('#d8dfea');
    expect(vars.variables['--arc-header']).toBe('#3b5998');
    expect(vars.light['--mantine-color-body']).toBe('#ffffff');
    expect(vars.light['--mantine-color-default-border']).toBe('#d8dfea');
    expect(vars.dark['--mantine-color-body']).toBe('#ffffff');
  });
});
