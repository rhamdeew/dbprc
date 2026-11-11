import { Autocomplete, Avatar, Box, Group, Text } from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import { useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { searchQueryReady, useColleagueSearch, type Colleague } from '../api/search';

const searchDebounceMs = 250;

function ColleagueOption({ person }: { person: Colleague }) {
  return (
    <Group gap="xs" wrap="nowrap">
      <Avatar src={person.avatarUrl} name={person.name} alt="" size={24} radius={2} color="arcBlue" />
      <div style={{ minWidth: 0 }}>
        <Text size="sm" fw={700} truncate>
          {person.name}
        </Text>
        <Text size="xs" c="dimmed" truncate>
          {person.email}
        </Text>
      </div>
    </Group>
  );
}

/** Colleague search in the top bar. Selecting a person opens their profile. */
export function SearchSlot() {
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [debounced] = useDebouncedValue(query, searchDebounceMs);
  const search = useColleagueSearch(debounced);
  const picking = useRef(false);
  const people = searchQueryReady(debounced) ? (search.data ?? []) : [];
  const byId = new Map(people.map((person) => [String(person.id), person]));

  return (
    <Box role="search" aria-label="Search" miw={180} w={220}>
      <Autocomplete
        aria-label="Search colleagues"
        placeholder="Search colleagues"
        size="xs"
        w="100%"
        value={query}
        data={people.map((person) => ({
          value: String(person.id),
          label: `${person.name} ${person.email}`,
        }))}
        filter={({ options }) => options}
        withScrollArea={false}
        comboboxProps={{ width: 320, position: 'bottom-end' }}
        styles={{ dropdown: { maxHeight: 320, overflowY: 'auto' } }}
        onChange={(next) => {
          if (picking.current) {
            picking.current = false;
            setQuery('');
            return;
          }
          setQuery(next);
        }}
        onOptionSubmit={(id) => {
          picking.current = true;
          setQuery('');
          navigate(`/u/${id}`);
        }}
        renderOption={({ option }) => {
          const person = byId.get(option.value);
          if (!person) return option.value;
          return <ColleagueOption person={person} />;
        }}
      />
    </Box>
  );
}
