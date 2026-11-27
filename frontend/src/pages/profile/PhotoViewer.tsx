import Lightbox, { CarouselModule, createModule, useLightboxState, type Plugin } from 'yet-another-react-lightbox';
import 'yet-another-react-lightbox/styles.css';
import { useProfile } from '../../api/profile';
import type { ProfilePhoto } from '../../api/profile';
import { PhotoInteractions } from './PhotoInteractions';
import './PhotoViewer.css';

// The carousel spans adjacent slides. Clip them at the edge of the image pane.
const imagePane: Plugin = ({ addParent }) => {
  addParent(CarouselModule.name, createModule('arc-image-pane', ({ children }) => (
    <div className="arc-photo-image-pane">{children}</div>
  )));
};

function SelectedPhoto({ userId, photos }: { userId: number; photos: ProfilePhoto[] }) {
  const { currentIndex } = useLightboxState();
  const photo = photos[currentIndex];
  return photo && <PhotoInteractions key={photo.id} userId={userId} photo={photo} />;
}

export function PhotoViewer({ userId, index, setIndex }: {
  userId: number;
  index: number;
  setIndex: (index: number) => void;
}) {
  // Opening adds a query observer so the gallery summaries refresh along with the comments.
  const { data: profile } = useProfile(userId);
  const photos = profile?.photos ?? [];
  return (
    <Lightbox
      className="arc-photo-viewer"
      plugins={[imagePane]}
      open
      close={() => setIndex(-1)}
      index={index}
      slides={photos.map((photo, photoIndex) => ({
        src: photo.url,
        alt: `Photo ${photoIndex + 1} of ${photos.length}`,
      }))}
      on={{ view: ({ index: current }) => setIndex(current) }}
      animation={{ fade: 0, swipe: 0 }}
      carousel={{ finite: true }}
      controller={{ touchAction: 'pan-y', preventDefaultWheelX: false, preventDefaultWheelY: false }}
      render={{ controls: () => <SelectedPhoto userId={userId} photos={photos} /> }}
    />
  );
}
