create table if not exists users(
  id serial primary key,
  email text unique not null,
  password_hash text not null default '',
  role text not null default 'admin'
);

create table if not exists devices(
  id serial primary key,
  name text not null,
  location text,
  is_locked boolean not null default true
);

create table if not exists events(
  id serial primary key,
  device_id int not null references devices(id) on delete cascade,
  action text not null,
  created_at timestamptz not null default now()
);

insert into devices(name, location, is_locked) values
  ('Main Entrance', 'Lobby', true),
  ('Server Room', 'Level 2', true)
on conflict do nothing;
