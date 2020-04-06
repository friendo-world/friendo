<?php

namespace Friendo;

require_once FRIENDO_PLUGIN_PATH . 'inc/post-types/types/class-block.php';
require_once FRIENDO_PLUGIN_PATH . 'inc/post-types/types/class-profile.php';

use Friendo\PostTypes\Block;
use Friendo\PostTypes\Profile;

class PostTypes
{
    protected $postTypes = [
        Block::class,
        Profile::class,
    ];

    public function init ()
    {
        $postTypes = new self;
        $postTypes->loadPostTypes();
        $postTypes->modifyPostTypes();
    }

    protected function loadPostTypes ()
    {
        foreach ( $this->postTypes as $postType ) {
			$class = new $postType();
			$class->loadPostType();
		}
    }

    protected function modifyPostTypes ()
    {
        add_action( 'init', [$this, 'removePostTypeSupport'], 99 );
    }

    final public function removePostTypeSupport ()
    {
        remove_post_type_support( 'post', 'editor' );
        remove_post_type_support( 'page', 'editor' );
    }
}