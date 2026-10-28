import React from 'react';
import {Composition,registerRoot} from 'remotion';
import {CollaborationFilm} from './collaboration-film-v2';
registerRoot(() => <Composition id="SiliecoCollaboration" component={CollaborationFilm} durationInFrames={1920} fps={30} width={1920} height={1080}/>);
