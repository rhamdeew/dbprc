import { createTheme } from '@mantine/core';

// "2006 social network" look. ARC-14 extends this.
export const theme = createTheme({
  primaryColor: 'arcBlue',
  colors: {
    arcBlue: [
      '#edeff4', '#d8dfea', '#b4c2db', '#8fa4cb', '#6b86bb',
      '#4c6aaa', '#3b5998', '#2f4779', '#23355b', '#17233c',
    ],
  },
  fontFamily: 'Tahoma, Verdana, sans-serif',
  defaultRadius: 2,
});
